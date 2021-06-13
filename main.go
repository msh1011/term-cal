package main

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gauth2 "google.golang.org/api/oauth2/v2"
	"google.golang.org/api/option"
)

var (
	prodHostname = "https://term-cal.herokuapp.com"

	oauthStateString  = "hrduislbnrdlivrnel"
	decoder           = schema.NewDecoder()
	googleOauthConfig *oauth2.Config
	userStore         *UserDataCache
)

func main() {
	start(prodHostname)
}

func start(host string) {
	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("PORT not set, exiting")
	}
	googleKey := os.Getenv("GOOGLE_KEY")
	googleSecret := os.Getenv("GOOGLE_SECRET")

	if googleKey == "" || googleSecret == "" {
		log.Fatal("GOOGLE_SECRET or GOOGLE_KEY not set, exiting")
	}

	db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}

	userStore = &UserDataCache{
		cache: map[string]UserData{},
		db:    db,
	}
	userStore.Init()

	googleOauthConfig = &oauth2.Config{
		RedirectURL:  host + "/auth/google/callback",
		ClientID:     googleKey,
		ClientSecret: googleSecret,
		Scopes: []string{
			"https://www.googleapis.com/auth/calendar.readonly",
			"https://www.googleapis.com/auth/userinfo.email",
		},
		Endpoint: google.Endpoint,
	}

	r := mux.NewRouter()

	r.HandleFunc("/cal/{uuid}", handleCal)

	r.HandleFunc("/", handleGoogleLogin)
	r.HandleFunc("/auth/login", handleGoogleLogin)
	r.HandleFunc("/auth/google/callback", handleGoogleCallback)

	loggedRouter := handlers.LoggingHandler(os.Stdout, r)

	fmt.Printf("Starting on port %s\n", port)
	fmt.Println(http.ListenAndServe(fmt.Sprintf(":%s", port), loggedRouter))
}

func handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	url := googleOauthConfig.AuthCodeURL(oauthStateString, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	state := r.FormValue("state")
	if state != oauthStateString {
		fmt.Printf("invalid oauth state, expected '%s', got '%s'\n", oauthStateString, state)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	token, err := googleOauthConfig.Exchange(oauth2.NoContext,
		r.FormValue("code"), oauth2.AccessTypeOffline)
	if err != nil {
		fmt.Printf("oauthConf.Exchange() failed with '%s'\n", err)
		return
	}
	ctx := context.Background()

	ser, err := gauth2.NewService(ctx, option.WithTokenSource(
		googleOauthConfig.TokenSource(ctx, token)))
	if err != nil {
		fmt.Printf("gauth2.NewService() failed with '%s'\n", err)
		return
	}
	info, err := gauth2.NewUserinfoService(ser).Get().Do()
	if err != nil {
		fmt.Printf("gauth2.NewUserinfoService() failed with '%s'\n", err)
		return
	}

	h := sha1.New()
	h.Write([]byte(info.Id))
	sh := h.Sum(nil)

	uuid := fmt.Sprintf("%x-%x-%x-%x", sh[0:5], sh[5:10], sh[10:15], sh[15:])

	err = userStore.Add(UserData{ID: uuid, Token: *token})
	if err != nil {
		fmt.Printf("userStore.Add() failed with '%s'\n", err)
		return
	}

	http.Redirect(w, r, "/cal/"+uuid, 302)
}

func handleCal(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	uuid := params["uuid"]

	cr := NewCalendarRequest(uuid)

	err := decoder.Decode(cr, r.URL.Query())
	if err != nil {
		fmt.Println(err)
		return
	}

	err = cr.Prepare()
	if err != nil {
		fmt.Fprint(w, err)
		return
	}

	res, err := cr.Generate()
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Fprint(w, res)
}
