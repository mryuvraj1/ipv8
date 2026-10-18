package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type User struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Avatar      string    `json:"avatar,omitempty"`
	Role        string    `json:"role"`
	Address     string    `json:"address,omitempty"`
	Password    string    `json:"passwordHash,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Branding struct {
	Name string `json:"name"`
	Logo string `json:"logo,omitempty"`
}

type Session struct {
	UserID string    `json:"userId"`
	Expiry time.Time `json:"expiry"`
}

type Enrollment struct {
	UserID string    `json:"userId"`
	Expiry time.Time `json:"expiry"`
}

type Installation struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Name      string    `json:"name"`
	Platform  string    `json:"platform"`
	Secret    string    `json:"secretHash"`
	LastSeen  time.Time `json:"lastSeen,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type database struct {
	Users         map[string]User         `json:"users"`
	Sessions      map[string]Session      `json:"sessions"`
	Enrollments   map[string]Enrollment   `json:"enrollments"`
	Installations map[string]Installation `json:"installations"`
	Branding      Branding                `json:"branding"`
}

func newDatabase() database {
	return database{map[string]User{}, map[string]Session{}, map[string]Enrollment{}, map[string]Installation{}, Branding{Name: "IPv8"}}
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func (a *App) save(db database) error {
	if err := os.MkdirAll(filepath.Dir(a.path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(a.path), ".ipv8-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, a.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(a.path))
	if err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func (a *App) change(fn func(*database) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, err := json.Marshal(a.db)
	if err != nil {
		return err
	}
	next := newDatabase()
	if err := json.Unmarshal(b, &next); err != nil {
		return err
	}
	if err := fn(&next); err != nil {
		return err
	}
	if err := a.save(next); err != nil {
		return err
	}
	a.db = next
	return nil
}

func (a *App) snapshot() database {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.db
}

func load(path string) (database, error) {
	db := newDatabase()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return db, nil
	}
	if err != nil {
		return db, err
	}
	if err := json.Unmarshal(b, &db); err != nil {
		return db, fmt.Errorf("read IPv8 data file: %w", err)
	}
	if db.Users == nil || db.Sessions == nil || db.Enrollments == nil || db.Installations == nil {
		return db, errors.New("IPv8 data file is missing required collections")
	}
	if strings.TrimSpace(db.Branding.Name) == "" {
		db.Branding = Branding{Name: "IPv8"}
	}
	return db, nil
}
