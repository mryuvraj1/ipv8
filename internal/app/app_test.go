package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mryuvraj1/ipv8/internal/core"
)

func TestUserAdminFlowAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "ipv8.json")
	adminPassword := testPassword(t)
	userPassword := testPassword(t)
	secondPassword := testPassword(t)
	a, err := New(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.BootstrapAdmin("owner@example.com", adminPassword); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()

	user := newClient(t)
	register := request(t, user, server.URL, "POST", "/api/register", map[string]string{"email": "user@example.com", "password": userPassword, "displayName": "Yuvraj"})
	if register.Code != 200 {
		t.Fatalf("register status %d: %s", register.Code, register.Body)
	}
	var userResponse struct {
		User User `json:"user"`
	}
	_ = json.Unmarshal(register.Body.Bytes(), &userResponse)
	if userResponse.User.Role != "user" || userResponse.User.Email != "user@example.com" {
		t.Fatalf("bad registered user: %#v", userResponse.User)
	}
	if bytes.Contains(register.Body.Bytes(), []byte("passwordHash")) {
		t.Fatal("registration response exposed password hash field")
	}
	adminRegistration := request(t, newClient(t), server.URL, "POST", "/api/register", map[string]string{"email": "attacker@example.com", "password": userPassword, "displayName": "Attacker", "role": "admin"})
	if adminRegistration.Code != 400 {
		t.Fatalf("public admin registration status %d", adminRegistration.Code)
	}
	forbidden := request(t, user, server.URL, "GET", "/api/admin/users", nil)
	if forbidden.Code != 403 {
		t.Fatalf("user admin access = %d", forbidden.Code)
	}

	address := request(t, user, server.URL, "POST", "/api/address", map[string]any{})
	if address.Code != 200 {
		t.Fatalf("address status %d: %s", address.Code, address.Body)
	}
	var addr struct {
		Address string `json:"address"`
	}
	_ = json.Unmarshal(address.Body.Bytes(), &addr)
	if _, err := core.ParseAddress(addr.Address); err != nil {
		t.Fatalf("invalid generated address %q: %v", addr.Address, err)
	}
	again := request(t, user, server.URL, "POST", "/api/address", map[string]any{})
	if !bytes.Equal(address.Body.Bytes(), again.Body.Bytes()) {
		t.Fatal("address generation was not idempotent")
	}
	sim := request(t, user, server.URL, "POST", "/api/simulate", map[string]string{"destination": addr.Address, "payload": "hello from IPv8"})
	if sim.Code != 200 || !strings.Contains(sim.Body.String(), `"delivered":true`) {
		t.Fatalf("local simulation: %d %s", sim.Code, sim.Body)
	}
	badSim := request(t, user, server.URL, "POST", "/api/simulate", map[string]string{"destination": "ipv8:1:aaaaaaaaaaaaaaaaaaaaaaaaaa", "payload": "x"})
	if badSim.Code != 400 {
		t.Fatalf("invalid simulator destination status %d", badSim.Code)
	}

	avatar := tinyPNG(t)
	profile := request(t, user, server.URL, "PATCH", "/api/me", map[string]string{"displayName": "Changed Name", "avatar": avatar})
	if profile.Code != 200 || !strings.Contains(profile.Body.String(), "Changed Name") {
		t.Fatalf("profile update: %d %s", profile.Code, profile.Body)
	}
	badImage := request(t, user, server.URL, "PATCH", "/api/me", map[string]string{"displayName": "Changed Name", "avatar": "data:image/png;base64,aGVsbG8="})
	if badImage.Code != 400 {
		t.Fatalf("invalid avatar content status %d", badImage.Code)
	}

	second := newClient(t)
	secondReg := request(t, second, server.URL, "POST", "/api/register", map[string]string{"email": "other@example.com", "password": secondPassword, "displayName": "Other"})
	if secondReg.Code != 200 {
		t.Fatalf("second register: %d %s", secondReg.Code, secondReg.Body)
	}
	secondAddress := request(t, second, server.URL, "POST", "/api/address", map[string]any{})
	var secondAddr struct {
		Address string `json:"address"`
	}
	_ = json.Unmarshal(secondAddress.Body.Bytes(), &secondAddr)
	if addr.Address == secondAddr.Address {
		t.Fatal("addresses are not unique")
	}

	enrollment := request(t, user, server.URL, "POST", "/api/installations/token", map[string]any{})
	var token struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(enrollment.Body.Bytes(), &token)
	if enrollment.Code != 201 || token.Token == "" {
		t.Fatalf("token create: %d %s", enrollment.Code, enrollment.Body)
	}
	enrolled := request(t, newClient(t), server.URL, "POST", "/api/installations/enroll", map[string]string{"token": token.Token, "name": "Laptop", "platform": "linux/amd64"})
	var install struct{ ID, Secret string }
	_ = json.Unmarshal(enrolled.Body.Bytes(), &install)
	if enrolled.Code != 201 || install.ID == "" || install.Secret == "" {
		t.Fatalf("enroll: %d %s", enrolled.Code, enrolled.Body)
	}
	reused := request(t, newClient(t), server.URL, "POST", "/api/installations/enroll", map[string]string{"token": token.Token, "name": "Second", "platform": "linux/amd64"})
	if reused.Code != 401 {
		t.Fatalf("reused token status %d", reused.Code)
	}
	heartbeat := request(t, newClient(t), server.URL, "POST", "/api/agent/heartbeat", install)
	if heartbeat.Code != 200 {
		t.Fatalf("heartbeat: %d %s", heartbeat.Code, heartbeat.Body)
	}
	userInstalls := request(t, user, server.URL, "GET", "/api/installations", nil)
	if userInstalls.Code != 200 || !strings.Contains(userInstalls.Body.String(), `"online"`) {
		t.Fatalf("installation listing: %d %s", userInstalls.Code, userInstalls.Body)
	}
	secondList := request(t, second, server.URL, "GET", "/api/installations", nil)
	if strings.Contains(secondList.Body.String(), install.ID) {
		t.Fatal("user can see another account's installation")
	}
	wrongDelete := request(t, second, server.URL, "DELETE", "/api/installations/"+install.ID, nil)
	if wrongDelete.Code != 403 {
		t.Fatalf("cross-user installation delete = %d", wrongDelete.Code)
	}

	admin := newClient(t)
	login := request(t, admin, server.URL, "POST", "/api/admin/login", map[string]string{"email": "owner@example.com", "password": adminPassword})
	if login.Code != 200 {
		t.Fatalf("admin login: %d %s", login.Code, login.Body)
	}
	wrongRoleLogin := request(t, newClient(t), server.URL, "POST", "/api/login", map[string]string{"email": "owner@example.com", "password": adminPassword})
	if wrongRoleLogin.Code != 401 {
		t.Fatalf("admin used user login: %d", wrongRoleLogin.Code)
	}
	userBranding := request(t, user, server.URL, "PATCH", "/api/admin/branding", map[string]string{"name": "Not allowed", "logo": ""})
	if userBranding.Code != 403 {
		t.Fatalf("user changed admin branding: status %d", userBranding.Code)
	}
	branding := request(t, admin, server.URL, "PATCH", "/api/admin/branding", map[string]string{"name": "Northstar IPv8", "logo": avatar})
	if branding.Code != 200 {
		t.Fatalf("branding update: %d %s", branding.Code, branding.Body)
	}
	adminUsers := request(t, admin, server.URL, "GET", "/api/admin/users", nil)
	if adminUsers.Code != 200 || strings.Contains(adminUsers.Body.String(), "passwordHash") {
		t.Fatalf("admin users response leaked credentials: %s", adminUsers.Body)
	}

	if err := request(t, user, server.URL, "POST", "/api/logout", map[string]any{}).Code; err != 200 {
		t.Fatalf("logout status %d", err)
	}
	if got := request(t, user, server.URL, "GET", "/api/me", nil).Code; got != 401 {
		t.Fatalf("logged-out session status %d", got)
	}

	server.Close()
	reopened, err := New(path, false)
	if err != nil {
		t.Fatal(err)
	}
	restarted := httptest.NewServer(reopened.Handler())
	defer restarted.Close()
	if got := reopened.snapshot().Branding.Name; got != "Northstar IPv8" {
		t.Fatalf("branding did not persist: %q", got)
	}
	if got := reopened.snapshot().Branding.Logo; got != avatar {
		t.Fatal("branding logo did not persist")
	}
	if got := reopened.snapshot().Users[userResponse.User.ID].DisplayName; got != "Changed Name" {
		t.Fatalf("profile did not persist: %q", got)
	}
	if got := reopened.snapshot().Users[userResponse.User.ID].Avatar; got != avatar {
		t.Fatal("profile avatar did not persist")
	}
	if got := reopened.snapshot().Users[userResponse.User.ID].Address; got != addr.Address {
		t.Fatal("generated address did not persist")
	}
	if len(reopened.snapshot().Installations) != 1 {
		t.Fatal("installation did not persist")
	}

	loggedIn := newClient(t)
	loginAgain := request(t, loggedIn, restarted.URL, "POST", "/api/login", map[string]string{"email": "user@example.com", "password": userPassword})
	if loginAgain.Code != 200 {
		t.Fatalf("login after logout: %d %s", loginAgain.Code, loginAgain.Body)
	}
	revoked := request(t, loggedIn, restarted.URL, "DELETE", "/api/installations/"+install.ID, nil)
	if revoked.Code != 200 {
		t.Fatalf("owner revoke: %d %s", revoked.Code, revoked.Body)
	}
	rejectedHeartbeat := request(t, newClient(t), restarted.URL, "POST", "/api/agent/heartbeat", install)
	if rejectedHeartbeat.Code != 401 {
		t.Fatalf("revoked agent heartbeat status %d", rejectedHeartbeat.Code)
	}
}

func testPassword(t *testing.T) string {
	t.Helper()
	password, err := randomToken(24)
	if err != nil {
		t.Fatal(err)
	}
	return password
}

func newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

func tinyPNG(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func request(t *testing.T, client *http.Client, base, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&b).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, base+path, &b)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rr := httptest.NewRecorder()
	rr.Code = resp.StatusCode
	for k, vs := range resp.Header {
		rr.Header()[k] = vs
	}
	_, _ = rr.Body.ReadFrom(resp.Body)
	return rr
}
