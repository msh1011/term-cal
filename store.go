package main

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	_ "github.com/lib/pq"
	"golang.org/x/oauth2"
)

var (
	createTableSQL = `
CREATE TABLE IF NOT EXISTS users (
  uuid TEXT PRIMARY KEY,
  data TEXT
)`
	insertUserDataSQL = `
INSERT INTO users (uuid, data)
VALUES ($1, $2)
ON CONFLICT (uuid)
DO
  UPDATE SET data = EXCLUDED.data
`
	readUserDataSQL = `
SELECT data FROM users
WHERE uuid = $1
`
)

type UserData struct {
	ID    string
	Token oauth2.Token
}

type UserDataCache struct {
	cache map[string]UserData
	db    *sql.DB
}

func (c *UserDataCache) Init() error {
	_, err := c.db.Exec(createTableSQL)
	if err != nil {
		return err
	}
	return nil
}

func (c *UserDataCache) Get(k string) (*UserData, error) {
	if val, ok := c.cache[k]; ok {
		return &val, nil
	}
	var data UserData
	err := c.db.QueryRow(readUserDataSQL, k).Scan(&data)
	if err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *UserDataCache) Add(u UserData) error {
	_, err := c.db.Exec(insertUserDataSQL, u.ID, u)
	if err != nil {
		return err
	}
	c.cache[u.ID] = u
	return nil
}

func (u UserData) Value() (driver.Value, error) {
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(u)
	return buf.String(), nil
}

func (u *UserData) Scan(value interface{}) error {
	if value == nil {
		*u = UserData{ID: "invalid"}
		return nil
	}
	if bv, err := driver.String.ConvertValue(value); err == nil {
		if v, ok := bv.(string); ok {
			buf := bytes.NewBufferString(v)
			err = json.NewDecoder(buf).Decode(&u)
			if err != nil {
				return fmt.Errorf("Error getting credentials")
			}
			return nil
		}
	}
	return errors.New("failed to scan UserData")
}
