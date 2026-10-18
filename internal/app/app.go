package app

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mryuvraj1/ipv8/internal/core"
	"github.com/mryuvraj1/ipv8/internal/dnsrecord"
	"github.com/mryuvraj1/ipv8/web"
)

const sessionDuration = 12 * time.Hour

type App struct {
	mu           sync.RWMutex
	path         string
	db           database
	cookieSecure bool
	publicURL    string
	apiURL       string
	dnsLookup    dnsrecord.TXTLookup
	rates        *rateLimiter
}

type rateLimiter struct {
	mu      sync.Mutex
	windows map[string]rateWindow
}

type rateWindow struct {
	start time.Time
	count int
}

func New(dataPath string, cookieSecure bool) (*App, error) {
	return NewWithOptions(dataPath, cookieSecure, "", "")
}

func NewWithOptions(dataPath string, cookieSecure bool, publicURL, apiURL string) (*App, error) {
	var err error
	publicURL, err = normalizePublicURL(publicURL)
	if err != nil {
		return nil, fmt.Errorf("IPV8_PUBLIC_URL: %w", err)
	}
	apiURL, err = normalizePublicURL(apiURL)
	if err != nil {
		return nil, fmt.Errorf("IPV8_API_URL: %w", err)
	}
	if publicURL == "" && apiURL != "" {
		return nil, errors.New("IPV8_API_URL requires IPV8_PUBLIC_URL to be configured")
	}
	if publicURL != "" && apiURL != "" && publicURL != apiURL {
		return nil, errors.New("IPV8_PUBLIC_URL and IPV8_API_URL must match for the current same-origin panel")
	}
	path, err := filepath.Abs(dataPath)
	if err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("IPv8 data file must not be a symlink")
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("IPv8 data path must be a regular file")
		}
		if info.Mode().Perm()&0077 != 0 {
			if err := os.Chmod(path, 0600); err != nil {
				return nil, fmt.Errorf("secure IPv8 data file permissions: %w", err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	db, err := load(path)
	if err != nil {
		return nil, err
	}
	a := &App{path: path, cookieSecure: cookieSecure, publicURL: publicURL, apiURL: apiURL, rates: &rateLimiter{windows: map[string]rateWindow{}}}
	changed, err := migrateAddresses(&db)
	if err != nil {
		return nil, fmt.Errorf("migrate IPv8 addresses: %w", err)
	}
	if changed {
		if err := a.save(db); err != nil {
			return nil, fmt.Errorf("save migrated IPv8 addresses: %w", err)
		}
	}
	a.db = db
	return a, nil
}

func (a *App) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]any{"status": "ok"}) })
	m.HandleFunc("GET /api/branding", a.getBranding)
	m.HandleFunc("GET /api/runtime-config", a.runtimeConfig)
	m.Handle("POST /api/register", a.limit(8, a.register))
	m.Handle("POST /api/login", a.limit(10, a.userLogin))
	m.Handle("POST /api/admin/login", a.limit(10, a.adminLogin))
	m.HandleFunc("POST /api/logout", a.logout)
	m.HandleFunc("GET /api/me", a.me)
	m.HandleFunc("PATCH /api/me", a.updateProfile)
	m.HandleFunc("POST /api/address", a.generateAddress)
	m.HandleFunc("GET /api/config", a.getConfig)
	m.HandleFunc("POST /api/simulate", a.simulate)
	m.Handle("POST /api/installations/token", a.limit(10, a.newEnrollment))
	m.HandleFunc("GET /api/installations", a.listInstallations)
	m.HandleFunc("GET /api/my-ipv8", a.myIPv8)
	m.HandleFunc("DELETE /api/installations/{id}", a.deleteInstallation)
	m.Handle("POST /api/installations/enroll", a.limit(12, a.enroll))
	m.Handle("POST /api/agent/heartbeat", a.limit(60, a.heartbeat))
	m.HandleFunc("GET /api/admin/overview", a.adminOverview)
	m.HandleFunc("GET /api/admin/nodes", a.adminNodes)
	m.HandleFunc("GET /api/admin/nodes/{id}", a.adminNode)
	m.Handle("POST /api/admin/dns/verify", a.limit(5, a.verifyDNS))
	m.HandleFunc("GET /api/admin/users", a.adminUsers)
	m.HandleFunc("PATCH /api/admin/users/{id}", a.updateUserStatus)
	m.HandleFunc("PATCH /api/admin/branding", a.updateBranding)
	files := http.FileServer(http.FS(web.Files))
	m.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			b, _ := web.Files.ReadFile("index.html")
			http.ServeContent(w, r, "index.html", time.Time{}, strings.NewReader(string(b)))
			return
		}
		files.ServeHTTP(w, r)
	})
	return securityHeaders(m)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(u.Host, r.Host) {
				writeError(w, http.StatusForbidden, "cross-origin request rejected")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func normalizePublicURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("must be an HTTP(S) origin without credentials, path, query, or fragment")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return "", errors.New("HTTPS is required except for localhost prototypes")
	}
	return u.String(), nil
}

func (a *App) runtimeConfig(w http.ResponseWriter, _ *http.Request) {
	apiURL := a.apiURL
	if apiURL == "" {
		apiURL = a.publicURL
	}
	writeJSON(w, 200, map[string]string{"publicURL": a.publicURL, "apiURL": apiURL})
}

func (a *App) limit(max int, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		key := r.URL.Path + ":" + ip
		now := time.Now()
		a.rates.mu.Lock()
		for k, v := range a.rates.windows {
			if now.Sub(v.start) > time.Minute {
				delete(a.rates.windows, k)
			}
		}
		v := a.rates.windows[key]
		if now.Sub(v.start) >= time.Minute || v.start.IsZero() {
			v = rateWindow{start: now}
		}
		v.count++
		a.rates.windows[key] = v
		a.rates.mu.Unlock()
		if v.count > max {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if r.ContentLength > 1<<20 {
		return errors.New("request body is too large")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func validEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > 254 {
		return "", errors.New("invalid email address")
	}
	a, err := mail.ParseAddress(value)
	if err != nil || a.Address != value || !strings.Contains(value, ".") {
		return "", errors.New("invalid email address")
	}
	return value, nil
}

var validName = regexp.MustCompile(`^[^\x00-\x1f\x7f]{1,60}$`)
var validPlatform = regexp.MustCompile(`^[a-zA-Z0-9._/-]{1,40}$`)
var validVersion = regexp.MustCompile(`^[a-zA-Z0-9.+_-]{1,32}$`)

func validateImage(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 || !(parts[0] == "data:image/png;base64" || parts[0] == "data:image/jpeg;base64") {
		return "", errors.New("image must be PNG or JPEG data")
	}
	b, err := base64.StdEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(b) == 0 || len(b) > 128<<10 {
		return "", errors.New("image must be valid and no larger than 128 KiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || (format != "png" && format != "jpeg") || parts[0] != "data:image/"+format+";base64" || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 16<<20 {
		return "", errors.New("image must be a valid PNG or JPEG no larger than 4096 by 4096 pixels")
	}
	return value, nil
}

func (a *App) getBranding(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, a.snapshot().Branding)
}

func (a *App) userBySession(r *http.Request) (User, bool) {
	c, err := r.Cookie("ipv8_session")
	if err != nil {
		return User{}, false
	}
	db := a.snapshot()
	s, ok := db.Sessions[tokenHash(c.Value)]
	if !ok || time.Now().After(s.Expiry) {
		return User{}, false
	}
	u, ok := db.Users[s.UserID]
	return u, ok && !u.Disabled
}

func (a *App) require(w http.ResponseWriter, r *http.Request, role string) (User, bool) {
	u, ok := a.userBySession(r)
	if !ok {
		writeError(w, 401, "authentication required")
		return User{}, false
	}
	if role != "" && u.Role != role {
		writeError(w, 403, "permission denied")
		return User{}, false
	}
	return u, true
}

func (a *App) register(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password, DisplayName string }
	if decodeJSON(w, r, &in) != nil {
		writeError(w, 400, "invalid JSON request")
		return
	}
	email, err := validEmail(in.Email)
	if err != nil || len(in.Password) < 10 || len(in.Password) > 256 {
		writeError(w, 400, "valid email and password (10–256 characters) are required")
		return
	}
	name := strings.TrimSpace(in.DisplayName)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	if !validName.MatchString(name) {
		writeError(w, 400, "display name must be 1–60 characters")
		return
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		writeError(w, 500, "could not secure password")
		return
	}
	u := User{Email: email, DisplayName: name, Role: "user", Password: hash, CreatedAt: time.Now().UTC()}
	err = a.change(func(db *database) error {
		for _, old := range db.Users {
			if old.Email == email {
				return errConflict
			}
		}
		for {
			id, err := randomToken(16)
			if err != nil {
				return err
			}
			if _, exists := db.Users[id]; !exists {
				u.ID = id
				break
			}
		}
		db.Users[u.ID] = u
		return nil
	})
	if errors.Is(err, errConflict) {
		writeError(w, 409, "an account with that email already exists")
		return
	}
	if err != nil {
		writeError(w, 500, "could not create account")
		return
	}
	a.createSession(w, u)
}

var errConflict = errors.New("conflict")

func (a *App) userLogin(w http.ResponseWriter, r *http.Request)  { a.login(w, r, "user") }
func (a *App) adminLogin(w http.ResponseWriter, r *http.Request) { a.login(w, r, "admin") }

func (a *App) login(w http.ResponseWriter, r *http.Request, role string) {
	var in struct{ Email, Password string }
	if decodeJSON(w, r, &in) != nil {
		writeError(w, 400, "invalid JSON request")
		return
	}
	email, err := validEmail(in.Email)
	if err != nil || len(in.Password) > 256 {
		writeError(w, 401, "email or password is incorrect")
		return
	}
	db := a.snapshot()
	var found User
	for _, u := range db.Users {
		if u.Email == email && u.Role == role {
			found = u
			break
		}
	}
	hash := found.Password
	if found.ID == "" || found.Disabled {
		hash = "pbkdf2-sha256$600000$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	}
	if !verifyPassword(in.Password, hash) || found.ID == "" || found.Disabled {
		writeError(w, 401, "email or password is incorrect")
		return
	}
	a.createSession(w, found)
}

func (a *App) createSession(w http.ResponseWriter, u User) {
	token, err := randomToken(32)
	if err != nil {
		writeError(w, 500, "could not start session")
		return
	}
	expiry := time.Now().Add(sessionDuration)
	if err := a.change(func(db *database) error {
		current, exists := db.Users[u.ID]
		if !exists || current.Disabled || current.Role != u.Role {
			return errUnauthorized
		}
		u = current
		for k, s := range db.Sessions {
			if time.Now().After(s.Expiry) {
				delete(db.Sessions, k)
			}
		}
		db.Sessions[tokenHash(token)] = Session{UserID: u.ID, Expiry: expiry}
		return nil
	}); errors.Is(err, errUnauthorized) {
		writeError(w, 401, "account is unavailable")
		return
	} else if err != nil {
		writeError(w, 500, "could not start session")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "ipv8_session", Value: token, Path: "/", HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteStrictMode, Expires: expiry, MaxAge: int(sessionDuration.Seconds())})
	writeJSON(w, 200, map[string]any{"user": publicUser(u)})
}

func publicUser(u User) User { u.Password = ""; return u }

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	var changeErr error
	if c, err := r.Cookie("ipv8_session"); err == nil {
		changeErr = a.change(func(db *database) error { delete(db.Sessions, tokenHash(c.Value)); return nil })
	}
	http.SetCookie(w, &http.Cookie{Name: "ipv8_session", Value: "", Path: "/", HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	if changeErr != nil {
		writeError(w, 500, "could not revoke session")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) me(w http.ResponseWriter, r *http.Request) {
	u, ok := a.require(w, r, "")
	if ok {
		writeJSON(w, 200, map[string]any{"user": publicUser(u)})
	}
}

func (a *App) updateProfile(w http.ResponseWriter, r *http.Request) {
	u, ok := a.require(w, r, "user")
	if !ok {
		return
	}
	var in struct {
		DisplayName string
		Avatar      string
	}
	if decodeJSON(w, r, &in) != nil {
		writeError(w, 400, "invalid JSON request")
		return
	}
	name := strings.TrimSpace(in.DisplayName)
	avatar, err := validateImage(in.Avatar)
	if !validName.MatchString(name) || err != nil {
		writeError(w, 400, "invalid display name or image")
		return
	}
	err = a.change(func(db *database) error {
		saved := db.Users[u.ID]
		saved.DisplayName, saved.Avatar = name, avatar
		db.Users[u.ID] = saved
		u = saved
		return nil
	})
	if err != nil {
		writeError(w, 500, "could not save profile")
		return
	}
	writeJSON(w, 200, map[string]any{"user": publicUser(u)})
}

func (a *App) generateAddress(w http.ResponseWriter, r *http.Request) {
	u, ok := a.require(w, r, "user")
	if !ok {
		return
	}
	err := a.change(func(db *database) error {
		saved := db.Users[u.ID]
		if saved.Address != "" {
			u = saved
			return nil
		}
		used := make(map[string]struct{}, len(db.Users))
		for _, other := range db.Users {
			if other.Address != "" {
				used[other.Address] = struct{}{}
			}
		}
		addr, err := allocateAddress(used, core.GenerateAddress)
		if err != nil {
			return err
		}
		saved.Address = addr.String()
		db.Users[u.ID] = saved
		u = saved
		return nil
	})
	if err != nil {
		writeError(w, 500, "could not generate address")
		return
	}
	writeJSON(w, 200, map[string]string{"address": u.Address})
}

func allocateAddress(used map[string]struct{}, generate func() (core.Address, error)) (core.Address, error) {
	for range 10 {
		address, err := generate()
		if err != nil {
			return core.Address{}, err
		}
		if address.Reserved() {
			continue
		}
		if _, exists := used[address.String()]; !exists {
			return address, nil
		}
	}
	return core.Address{}, errors.New("address collision limit reached")
}

func migrateAddresses(db *database) (bool, error) {
	ids := make([]string, 0, len(db.Users))
	for id := range db.Users {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	used := make(map[string]struct{}, len(db.Users))
	var pending []string
	for _, id := range ids {
		user := db.Users[id]
		if user.Address == "" {
			continue
		}
		parsed, err := core.ParseAddress(user.Address)
		if err != nil || parsed.String() != user.Address {
			pending = append(pending, id)
			continue
		}
		if _, exists := used[user.Address]; exists {
			pending = append(pending, id)
			continue
		}
		used[user.Address] = struct{}{}
	}
	for _, id := range pending {
		address, err := allocateAddress(used, core.GenerateAddress)
		if err != nil {
			return false, err
		}
		user := db.Users[id]
		user.Address = address.String()
		db.Users[id] = user
		used[user.Address] = struct{}{}
	}
	return len(pending) > 0, nil
}

func (a *App) getConfig(w http.ResponseWriter, r *http.Request) {
	u, ok := a.require(w, r, "user")
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"experimental": true, "address": u.Address, "network": "local-prototype-only", "note": "No public routing or transport is implemented."})
}

func (a *App) simulate(w http.ResponseWriter, r *http.Request) {
	u, ok := a.require(w, r, "user")
	if !ok {
		return
	}
	if u.Address == "" {
		writeError(w, 409, "generate an address first")
		return
	}
	var in struct {
		Destination string
		Payload     string
	}
	if decodeJSON(w, r, &in) != nil {
		writeError(w, 400, "invalid JSON request")
		return
	}
	dest, err := core.ParseAddress(in.Destination)
	if err != nil || in.Destination != u.Address || len([]byte(in.Payload)) > core.MaxPayload {
		writeError(w, 400, "destination must be your valid IPv8 address and payload must be at most 1200 bytes")
		return
	}
	source, _ := core.ParseAddress(u.Address)
	b, err := (core.Packet{Source: source, Destination: dest, Payload: []byte(in.Payload)}).MarshalBinary()
	if err != nil {
		writeError(w, 400, "packet validation failed")
		return
	}
	p, err := core.UnmarshalPacket(b)
	if err != nil {
		writeError(w, 500, "local packet validation failed")
		return
	}
	writeJSON(w, 200, map[string]any{"delivered": true, "mode": "local-loopback", "packetBytes": len(b), "payload": string(p.Payload)})
}

func (a *App) newEnrollment(w http.ResponseWriter, r *http.Request) {
	u, ok := a.require(w, r, "user")
	if !ok {
		return
	}
	token, err := randomToken(32)
	if err != nil {
		writeError(w, 500, "could not create enrollment token")
		return
	}
	expiry := time.Now().Add(15 * time.Minute)
	if err := a.change(func(db *database) error {
		for key, enrollment := range db.Enrollments {
			if enrollment.UserID == u.ID || time.Now().After(enrollment.Expiry) {
				delete(db.Enrollments, key)
			}
		}
		db.Enrollments[tokenHash(token)] = Enrollment{UserID: u.ID, Expiry: expiry}
		return nil
	}); err != nil {
		writeError(w, 500, "could not create enrollment token")
		return
	}
	writeJSON(w, 201, map[string]any{"token": token, "expiresAt": expiry})
}

func (a *App) enroll(w http.ResponseWriter, r *http.Request) {
	var in struct{ Token, Name, Platform, Version string }
	if decodeJSON(w, r, &in) != nil || len(in.Token) > 128 {
		writeError(w, 400, "invalid enrollment request")
		return
	}
	name := strings.TrimSpace(in.Name)
	platform := strings.TrimSpace(in.Platform)
	version := strings.TrimSpace(in.Version)
	if !validName.MatchString(name) || !validPlatform.MatchString(platform) || (version != "" && !validVersion.MatchString(version)) {
		writeError(w, 400, "valid installation name and platform are required")
		return
	}
	id, err := randomToken(16)
	if err != nil {
		writeError(w, 500, "could not enroll installation")
		return
	}
	secret, err := randomToken(32)
	if err != nil {
		writeError(w, 500, "could not enroll installation")
		return
	}
	inst := Installation{ID: id, Name: name, Platform: platform, Version: version, Secret: tokenHash(secret), CreatedAt: time.Now().UTC()}
	err = a.change(func(db *database) error {
		key := tokenHash(in.Token)
		enrollment, exists := db.Enrollments[key]
		if !exists || time.Now().After(enrollment.Expiry) {
			delete(db.Enrollments, key)
			return errUnauthorized
		}
		inst.UserID = enrollment.UserID
		delete(db.Enrollments, key)
		db.Installations[id] = inst
		return nil
	})
	if errors.Is(err, errUnauthorized) {
		writeError(w, 401, "enrollment token is invalid, expired, or already used")
		return
	}
	if err != nil {
		writeError(w, 500, "could not enroll installation")
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "secret": secret, "heartbeatSeconds": 30})
}

var errUnauthorized = errors.New("unauthorized")

func (a *App) heartbeat(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID, Secret string }
	if decodeJSON(w, r, &in) != nil {
		writeError(w, 400, "invalid heartbeat request")
		return
	}
	err := a.change(func(db *database) error {
		i, exists := db.Installations[in.ID]
		if !exists || subtle.ConstantTimeCompare([]byte(i.Secret), []byte(tokenHash(in.Secret))) != 1 {
			return errUnauthorized
		}
		i.LastSeen = time.Now().UTC()
		db.Installations[in.ID] = i
		return nil
	})
	if errors.Is(err, errUnauthorized) {
		writeError(w, 401, "invalid installation credentials")
		return
	}
	if err != nil {
		writeError(w, 500, "could not update installation status")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) listInstallations(w http.ResponseWriter, r *http.Request) {
	u, ok := a.require(w, r, "user")
	if !ok {
		return
	}
	db := a.snapshot()
	list := make([]map[string]any, 0)
	for _, i := range db.Installations {
		if i.UserID == u.ID {
			list = append(list, installationView(i))
		}
	}
	writeJSON(w, 200, map[string]any{"installations": list})
}

func (a *App) myIPv8(w http.ResponseWriter, r *http.Request) {
	u, ok := a.require(w, r, "user")
	if !ok {
		return
	}
	list := make([]map[string]any, 0)
	status := "not_installed"
	for _, i := range a.snapshot().Installations {
		if i.UserID != u.ID {
			continue
		}
		view := installationView(i)
		list = append(list, view)
		if status == "not_installed" {
			status = "offline"
		}
		if view["status"] == "online" {
			status = "online"
		}
	}
	writeJSON(w, 200, map[string]any{"status": status, "installations": list})
}

func installationView(i Installation) map[string]any {
	return map[string]any{"id": i.ID, "name": i.Name, "platform": i.Platform, "agentVersion": i.Version, "createdAt": i.CreatedAt, "lastSeen": i.LastSeen, "status": installationStatus(i.LastSeen, time.Now())}
}

func installationStatus(lastSeen, now time.Time) string {
	age := now.Sub(lastSeen)
	if !lastSeen.IsZero() && age >= 0 && age <= 90*time.Second {
		return "online"
	}
	return "offline"
}

func (a *App) deleteInstallation(w http.ResponseWriter, r *http.Request) {
	u, ok := a.require(w, r, "user")
	if !ok {
		return
	}
	err := a.change(func(db *database) error {
		i, exists := db.Installations[r.PathValue("id")]
		if exists && i.UserID != u.ID {
			return errForbidden
		}
		if exists {
			delete(db.Installations, i.ID)
		}
		return nil
	})
	if errors.Is(err, errForbidden) {
		writeError(w, 403, "permission denied")
		return
	}
	if err != nil {
		writeError(w, 500, "could not revoke installation")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

var errForbidden = errors.New("forbidden")

func (a *App) adminOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.require(w, r, "admin"); !ok {
		return
	}
	db := a.snapshot()
	online := 0
	for _, i := range db.Installations {
		if installationStatus(i.LastSeen, time.Now()) == "online" {
			online++
		}
	}
	writeJSON(w, 200, map[string]int{"users": len(db.Users), "installations": len(db.Installations), "online": online, "offline": len(db.Installations) - online})
}

func (a *App) adminNodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.require(w, r, "admin"); !ok {
		return
	}
	limit, offset := 50, 0
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			writeError(w, 400, "limit must be from 1 to 100")
			return
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 || offset > 1000000 {
			writeError(w, 400, "offset must be from 0 to 1000000")
			return
		}
	}
	db := a.snapshot()
	items := make([]Installation, 0, len(db.Installations))
	for _, item := range db.Installations {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	total := len(items)
	if offset >= total {
		items = items[:0]
	} else {
		items = items[offset:]
		if len(items) > limit {
			items = items[:limit]
		}
	}
	nodes := make([]map[string]any, 0, len(items))
	for _, item := range items {
		nodes = append(nodes, adminNodeView(item, db.Users[item.UserID]))
	}
	writeJSON(w, 200, map[string]any{"nodes": nodes, "total": total, "limit": limit, "offset": offset})
}

func (a *App) adminNode(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.require(w, r, "admin"); !ok {
		return
	}
	db := a.snapshot()
	item, exists := db.Installations[r.PathValue("id")]
	if !exists {
		writeError(w, 404, "node not found")
		return
	}
	writeJSON(w, 200, map[string]any{"node": adminNodeView(item, db.Users[item.UserID])})
}

func adminNodeView(i Installation, user User) map[string]any {
	return map[string]any{"id": i.ID, "address": user.Address, "name": i.Name, "user": user.Email, "platform": i.Platform, "agentVersion": i.Version, "createdAt": i.CreatedAt, "lastSeen": i.LastSeen, "status": installationStatus(i.LastSeen, time.Now())}
}

func (a *App) verifyDNS(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.require(w, r, "admin"); !ok {
		return
	}
	var in struct {
		Domain  string `json:"domain"`
		Address string `json:"address"`
	}
	if decodeJSON(w, r, &in) != nil {
		writeError(w, 400, "domain and address are required")
		return
	}
	domain, err := dnsrecord.ValidateDomain(in.Domain)
	if err != nil {
		writeError(w, 400, "domain must be a valid ASCII DNS name")
		return
	}
	address, err := core.ParseAddress(in.Address)
	if err != nil || address.String() != in.Address {
		writeError(w, 400, "address must be a canonical IPv8 experimental address")
		return
	}
	record, err := dnsrecord.Resolve(r.Context(), domain, a.dnsLookup, time.Now())
	if errors.Is(err, dnsrecord.ErrNotFound) {
		writeError(w, 404, "no IPv8 TXT record found at _ipv8."+domain)
		return
	}
	if errors.Is(err, dnsrecord.ErrInvalid) || errors.Is(err, dnsrecord.ErrUnsupported) || errors.Is(err, dnsrecord.ErrExpired) {
		writeError(w, 422, err.Error())
		return
	}
	if err != nil {
		writeError(w, 502, "DNS lookup failed; check the domain and resolver, then try again")
		return
	}
	writeJSON(w, 200, map[string]any{"record": record, "expectedAddress": in.Address, "verified": record.Address == in.Address})
}

func (a *App) adminUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.require(w, r, "admin"); !ok {
		return
	}
	db := a.snapshot()
	users := make([]User, 0, len(db.Users))
	for _, u := range db.Users {
		users = append(users, publicUser(u))
	}
	writeJSON(w, 200, map[string]any{"users": users})
}

func (a *App) updateUserStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.require(w, r, "admin"); !ok {
		return
	}
	var in struct {
		Disabled *bool `json:"disabled"`
	}
	if decodeJSON(w, r, &in) != nil || in.Disabled == nil {
		writeError(w, 400, "disabled must be a boolean")
		return
	}
	id := r.PathValue("id")
	err := a.change(func(db *database) error {
		u, exists := db.Users[id]
		if !exists {
			return errNotFound
		}
		if u.Role != "user" {
			return errForbidden
		}
		u.Disabled = *in.Disabled
		db.Users[id] = u
		if u.Disabled {
			for key, session := range db.Sessions {
				if session.UserID == id {
					delete(db.Sessions, key)
				}
			}
			for key, enrollment := range db.Enrollments {
				if enrollment.UserID == id {
					delete(db.Enrollments, key)
				}
			}
			for key, installation := range db.Installations {
				if installation.UserID == id {
					delete(db.Installations, key)
				}
			}
		}
		return nil
	})
	if errors.Is(err, errNotFound) {
		writeError(w, 404, "user not found")
		return
	}
	if errors.Is(err, errForbidden) {
		writeError(w, 403, "administrators cannot be suspended here")
		return
	}
	if err != nil {
		writeError(w, 500, "could not update user status")
		return
	}
	writeJSON(w, 200, map[string]any{"user": publicUser(a.snapshot().Users[id])})
}

var errNotFound = errors.New("not found")

func (a *App) updateBranding(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.require(w, r, "admin"); !ok {
		return
	}
	var in Branding
	if decodeJSON(w, r, &in) != nil {
		writeError(w, 400, "invalid JSON request")
		return
	}
	name := strings.TrimSpace(in.Name)
	logo, err := validateImage(in.Logo)
	if len(name) < 2 || len(name) > 40 || strings.ContainsAny(name, "<>\n\r") || err != nil {
		writeError(w, 400, "platform name must be 2–40 characters and logo must be a supported image under 128 KiB")
		return
	}
	brand := Branding{Name: name, Logo: logo}
	if err := a.change(func(db *database) error { db.Branding = brand; return nil }); err != nil {
		writeError(w, 500, "could not save branding")
		return
	}
	writeJSON(w, 200, brand)
}

func (a *App) BootstrapAdmin(email, password string) error {
	email, err := validEmail(email)
	if err != nil || len(password) < 14 || len(password) > 256 {
		return errors.New("admin bootstrap requires a valid email and password of at least 14 characters")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	u := User{Email: email, DisplayName: "Administrator", Role: "admin", Password: hash, CreatedAt: time.Now().UTC()}
	return a.change(func(db *database) error {
		for _, old := range db.Users {
			if old.Role == "admin" {
				return errors.New("an admin already exists")
			}
			if old.Email == email {
				return errConflict
			}
		}
		for {
			id, err := randomToken(16)
			if err != nil {
				return err
			}
			if _, exists := db.Users[id]; !exists {
				u.ID = id
				break
			}
		}
		db.Users[u.ID] = u
		return nil
	})
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := pbkdf2SHA256([]byte(password), salt, 600000, 32)
	return fmt.Sprintf("pbkdf2-sha256$600000$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != "600000" {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[2])
	expected, e2 := base64.RawStdEncoding.DecodeString(parts[3])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(expected) != 32 {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, 600000, 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	out := make([]byte, keyLength)
	var block [4]byte
	for offset, index := 0, uint32(1); offset < keyLength; index++ {
		block[0], block[1], block[2], block[3] = byte(index>>24), byte(index>>16), byte(index>>8), byte(index)
		h := hmacSHA256(password, append(append([]byte(nil), salt...), block[:]...))
		u, t := h, append([]byte(nil), h...)
		for i := 1; i < iterations; i++ {
			u = hmacSHA256(password, u)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		offset += copy(out[offset:], t)
	}
	return out
}

func hmacSHA256(key, data []byte) []byte {
	const block = 64
	if len(key) > block {
		sum := sha256.Sum256(key)
		key = sum[:]
	}
	k := make([]byte, block)
	copy(k, key)
	inner, outer := make([]byte, block), make([]byte, block)
	for i := range k {
		inner[i], outer[i] = k[i]^0x36, k[i]^0x5c
	}
	h := sha256.New()
	_, _ = h.Write(inner)
	_, _ = h.Write(data)
	first := h.Sum(nil)
	h.Reset()
	_, _ = h.Write(outer)
	_, _ = h.Write(first)
	return h.Sum(nil)
}
