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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if got := request(t, newClient(t), server.URL, "GET", "/api/my-ipv8", nil).Code; got != 401 {
		t.Fatalf("unauthenticated My IPv8 status = %d", got)
	}
	initialStatus := request(t, user, server.URL, "GET", "/api/my-ipv8", nil)
	if initialStatus.Code != 200 || !strings.Contains(initialStatus.Body.String(), `"status":"not_installed"`) {
		t.Fatalf("initial My IPv8 status: %d %s", initialStatus.Code, initialStatus.Body)
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
	var secondUserResponse struct {
		User User `json:"user"`
	}
	if err := json.Unmarshal(secondReg.Body.Bytes(), &secondUserResponse); err != nil || secondUserResponse.User.ID == "" {
		t.Fatalf("decode second account: %v", err)
	}
	secondAddress := request(t, second, server.URL, "POST", "/api/address", map[string]any{})
	var secondAddr struct {
		Address string `json:"address"`
	}
	_ = json.Unmarshal(secondAddress.Body.Bytes(), &secondAddr)
	if addr.Address == secondAddr.Address {
		t.Fatal("addresses are not unique")
	}
	secondTokenResponse := request(t, second, server.URL, "POST", "/api/installations/token", map[string]any{})
	var secondToken struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(secondTokenResponse.Body.Bytes(), &secondToken)
	secondEnrollment := request(t, newClient(t), server.URL, "POST", "/api/installations/enroll", map[string]string{"token": secondToken.Token, "name": "Other laptop", "platform": "linux/amd64"})
	var secondInstall struct{ ID, Secret string }
	_ = json.Unmarshal(secondEnrollment.Body.Bytes(), &secondInstall)
	if secondEnrollment.Code != 201 || secondInstall.ID == "" {
		t.Fatalf("second installation enroll: %d %s", secondEnrollment.Code, secondEnrollment.Body)
	}
	pendingResponse := request(t, second, server.URL, "POST", "/api/installations/token", map[string]any{})
	var pendingToken struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(pendingResponse.Body.Bytes(), &pendingToken)

	enrollment := request(t, user, server.URL, "POST", "/api/installations/token", map[string]any{})
	var token struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(enrollment.Body.Bytes(), &token)
	if enrollment.Code != 201 || token.Token == "" {
		t.Fatalf("token create: %d %s", enrollment.Code, enrollment.Body)
	}
	enrolled := request(t, newClient(t), server.URL, "POST", "/api/installations/enroll", map[string]string{"token": token.Token, "name": "Laptop", "platform": "linux/amd64", "version": "0.2.0"})
	var install struct{ ID, Secret string }
	_ = json.Unmarshal(enrolled.Body.Bytes(), &install)
	if enrolled.Code != 201 || install.ID == "" || install.Secret == "" {
		t.Fatalf("enroll: %d %s", enrolled.Code, enrolled.Body)
	}
	beforeHeartbeat := request(t, user, server.URL, "GET", "/api/my-ipv8", nil)
	if beforeHeartbeat.Code != 200 || !strings.Contains(beforeHeartbeat.Body.String(), `"status":"offline"`) {
		t.Fatalf("status before first heartbeat: %d %s", beforeHeartbeat.Code, beforeHeartbeat.Body)
	}
	invalidHeartbeat := request(t, newClient(t), server.URL, "POST", "/api/agent/heartbeat", map[string]string{"id": install.ID, "secret": "invalid"})
	if invalidHeartbeat.Code != 401 {
		t.Fatalf("invalid heartbeat status %d", invalidHeartbeat.Code)
	}
	stillOffline := request(t, user, server.URL, "GET", "/api/my-ipv8", nil)
	if !strings.Contains(stillOffline.Body.String(), `"status":"offline"`) {
		t.Fatalf("invalid heartbeat changed status: %s", stillOffline.Body)
	}
	reused := request(t, newClient(t), server.URL, "POST", "/api/installations/enroll", map[string]string{"token": token.Token, "name": "Second", "platform": "linux/amd64"})
	if reused.Code != 401 {
		t.Fatalf("reused token status %d", reused.Code)
	}
	heartbeat := request(t, newClient(t), server.URL, "POST", "/api/agent/heartbeat", install)
	if heartbeat.Code != 200 {
		t.Fatalf("heartbeat: %d %s", heartbeat.Code, heartbeat.Body)
	}
	myIPv8 := request(t, user, server.URL, "GET", "/api/my-ipv8", nil)
	if myIPv8.Code != 200 || !strings.Contains(myIPv8.Body.String(), `"status":"online"`) {
		t.Fatalf("status after valid heartbeat: %d %s", myIPv8.Code, myIPv8.Body)
	}
	userInstalls := request(t, user, server.URL, "GET", "/api/installations", nil)
	if userInstalls.Code != 200 || !strings.Contains(userInstalls.Body.String(), `"online"`) {
		t.Fatalf("installation listing: %d %s", userInstalls.Code, userInstalls.Body)
	}
	secondList := request(t, second, server.URL, "GET", "/api/installations", nil)
	if strings.Contains(secondList.Body.String(), install.ID) {
		t.Fatal("user can see another account's installation")
	}
	secondStatus := request(t, second, server.URL, "GET", "/api/my-ipv8", nil)
	if !strings.Contains(secondStatus.Body.String(), `"status":"offline"`) || strings.Contains(secondStatus.Body.String(), install.ID) || !strings.Contains(secondStatus.Body.String(), secondInstall.ID) {
		t.Fatalf("My IPv8 leaked another account's installation: %s", secondStatus.Body)
	}
	wrongDelete := request(t, second, server.URL, "DELETE", "/api/installations/"+install.ID, nil)
	if wrongDelete.Code != 403 {
		t.Fatalf("cross-user installation delete = %d", wrongDelete.Code)
	}
	if err := a.change(func(db *database) error {
		i := db.Installations[install.ID]
		i.LastSeen = time.Now().Add(-91 * time.Second)
		db.Installations[install.ID] = i
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	staleStatus := request(t, user, server.URL, "GET", "/api/my-ipv8", nil)
	if !strings.Contains(staleStatus.Body.String(), `"status":"offline"`) {
		t.Fatalf("status did not transition to offline after heartbeat expiry: %s", staleStatus.Body)
	}

	admin := newClient(t)
	if got := request(t, user, server.URL, "GET", "/api/admin/nodes", nil).Code; got != 403 {
		t.Fatalf("user admin-node access = %d", got)
	}
	login := request(t, admin, server.URL, "POST", "/api/admin/login", map[string]string{"email": "owner@example.com", "password": adminPassword})
	if login.Code != 200 {
		t.Fatalf("admin login: %d %s", login.Code, login.Body)
	}
	wrongRoleLogin := request(t, newClient(t), server.URL, "POST", "/api/login", map[string]string{"email": "owner@example.com", "password": adminPassword})
	if wrongRoleLogin.Code != 401 {
		t.Fatalf("admin used user login: %d", wrongRoleLogin.Code)
	}
	if got := request(t, admin, server.URL, "GET", "/api/my-ipv8", nil).Code; got != 403 {
		t.Fatalf("administrator used user My IPv8 endpoint: %d", got)
	}
	adminNodes := request(t, admin, server.URL, "GET", "/api/admin/nodes?limit=10&offset=0", nil)
	if adminNodes.Code != 200 || !strings.Contains(adminNodes.Body.String(), install.ID) || !strings.Contains(adminNodes.Body.String(), userResponse.User.Email) {
		t.Fatalf("admin node inventory: %d %s", adminNodes.Code, adminNodes.Body)
	}
	if got := request(t, admin, server.URL, "GET", "/api/admin/nodes?limit=101", nil).Code; got != 400 {
		t.Fatalf("admin node pagination limit = %d", got)
	}
	if got := request(t, user, server.URL, "GET", "/api/admin/nodes/"+install.ID, nil).Code; got != 403 {
		t.Fatalf("user node detail access = %d", got)
	}
	adminNode := request(t, admin, server.URL, "GET", "/api/admin/nodes/"+install.ID, nil)
	if adminNode.Code != 200 || !strings.Contains(adminNode.Body.String(), `"status":"offline"`) || !strings.Contains(adminNode.Body.String(), `"address":"`+addr.Address+`"`) || !strings.Contains(adminNode.Body.String(), `"agentVersion":"0.2.0"`) {
		t.Fatalf("admin node details: %d %s", adminNode.Code, adminNode.Body)
	}
	userBranding := request(t, user, server.URL, "PATCH", "/api/admin/branding", map[string]string{"name": "Not allowed", "logo": ""})
	if userBranding.Code != 403 {
		t.Fatalf("user changed admin branding: status %d", userBranding.Code)
	}
	userStatusChange := request(t, user, server.URL, "PATCH", "/api/admin/users/"+secondUserResponse.User.ID, map[string]bool{"disabled": true})
	if userStatusChange.Code != 403 {
		t.Fatalf("user changed another account status: %d", userStatusChange.Code)
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
	if len(reopened.snapshot().Installations) != 2 {
		t.Fatal("installations did not persist")
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
	adminID := ""
	for id, account := range reopened.snapshot().Users {
		if account.Role == "admin" {
			adminID = id
		}
	}
	if adminID == "" {
		t.Fatal("bootstrapped administrator not found")
	}
	selfSuspend := request(t, admin, restarted.URL, "PATCH", "/api/admin/users/"+adminID, map[string]bool{"disabled": true})
	if selfSuspend.Code != 403 {
		t.Fatalf("administrator suspension status %d", selfSuspend.Code)
	}
	missingStatus := request(t, admin, restarted.URL, "PATCH", "/api/admin/users/"+secondUserResponse.User.ID, map[string]string{})
	if missingStatus.Code != 400 {
		t.Fatalf("missing disabled field status %d", missingStatus.Code)
	}
	suspended := request(t, admin, restarted.URL, "PATCH", "/api/admin/users/"+secondUserResponse.User.ID, map[string]bool{"disabled": true})
	if suspended.Code != 200 || !strings.Contains(suspended.Body.String(), `"disabled":true`) {
		t.Fatalf("suspend user: %d %s", suspended.Code, suspended.Body)
	}
	if got := request(t, second, restarted.URL, "GET", "/api/me", nil).Code; got != 401 {
		t.Fatalf("suspended session status %d", got)
	}
	if got := request(t, newClient(t), restarted.URL, "POST", "/api/login", map[string]string{"email": "other@example.com", "password": secondPassword}).Code; got != 401 {
		t.Fatalf("suspended login status %d", got)
	}
	if got := request(t, newClient(t), restarted.URL, "POST", "/api/agent/heartbeat", secondInstall).Code; got != 401 {
		t.Fatalf("suspended installation heartbeat status %d", got)
	}
	if got := request(t, newClient(t), restarted.URL, "POST", "/api/installations/enroll", map[string]string{"token": pendingToken.Token, "name": "Late device", "platform": "linux/amd64"}).Code; got != 401 {
		t.Fatalf("suspended enrollment token status %d", got)
	}
	restored := request(t, admin, restarted.URL, "PATCH", "/api/admin/users/"+secondUserResponse.User.ID, map[string]bool{"disabled": false})
	if restored.Code != 200 || !strings.Contains(restored.Body.String(), `"disabled":false`) {
		t.Fatalf("restore user: %d %s", restored.Code, restored.Body)
	}
	if got := request(t, second, restarted.URL, "GET", "/api/me", nil).Code; got != 401 {
		t.Fatalf("restored user's old session became active: %d", got)
	}
	if got := request(t, newClient(t), restarted.URL, "POST", "/api/login", map[string]string{"email": "other@example.com", "password": secondPassword}).Code; got != 200 {
		t.Fatalf("restored user login status %d", got)
	}
}

func TestPublicURLConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	a, err := NewWithOptions(path, true, "https://panel.example.com", "https://panel.example.com")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	a.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/runtime-config", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"publicURL":"https://panel.example.com"`) || !strings.Contains(response.Body.String(), `"apiURL":"https://panel.example.com"`) {
		t.Fatalf("runtime configuration: %d %s", response.Code, response.Body)
	}
	for _, invalid := range []string{"http://panel.example.com", "https://user@panel.example.com", "https://panel.example.com/path", "https://panel.example.com?token=x"} {
		if _, err := NewWithOptions(filepath.Join(t.TempDir(), "state.json"), false, invalid, ""); err == nil {
			t.Errorf("accepted unsafe public URL %q", invalid)
		}
	}
	if _, err := NewWithOptions(filepath.Join(t.TempDir(), "state.json"), false, "https://panel.example.com", "https://api.example.com"); err == nil {
		t.Fatal("accepted split origin without CORS/cross-origin cookie support")
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

func TestInstallationStatusHeartbeatFreshness(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, test := range []struct {
		name string
		seen time.Time
		want string
	}{
		{name: "never seen", want: "offline"},
		{name: "fresh", seen: now.Add(-90 * time.Second), want: "online"},
		{name: "stale", seen: now.Add(-90*time.Second - time.Nanosecond), want: "offline"},
		{name: "future timestamp", seen: now.Add(time.Second), want: "offline"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := installationStatus(test.seen, now); got != test.want {
				t.Fatalf("installationStatus(%v) = %q, want %q", test.seen, got, test.want)
			}
		})
	}
}

func TestAddressAllocationRetriesCollisions(t *testing.T) {
	var collision, fresh, zero core.Address
	collision[0], fresh[0] = 1, 2
	used := map[string]struct{}{collision.String(): {}}
	calls := 0
	address, err := allocateAddress(used, func() (core.Address, error) {
		calls++
		if calls == 1 {
			return collision, nil
		}
		return fresh, nil
	})
	if err != nil || address != fresh || calls != 2 {
		t.Fatalf("collision retry returned %x after %d attempts: %v", address, calls, err)
	}
	if _, err := allocateAddress(used, func() (core.Address, error) { return collision, nil }); err == nil {
		t.Fatal("did not stop after repeated address collisions")
	}
	if _, err := allocateAddress(nil, func() (core.Address, error) { return zero, nil }); err == nil {
		t.Fatal("allocated a reserved address")
	}
}

func TestLegacyAddressMigrationIsUniqueAndPersistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "ipv8.json")
	db := newDatabase()
	legacy := "ipv8:1:" + strings.Repeat("a", 26)
	db.Users["first"] = User{ID: "first", Address: legacy}
	db.Users["second"] = User{ID: "second", Address: legacy}
	data, err := json.Marshal(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	first, err := New(path, false)
	if err != nil {
		t.Fatal(err)
	}
	a, b := first.snapshot().Users["first"].Address, first.snapshot().Users["second"].Address
	parsedA, errA := core.ParseAddress(a)
	parsedB, errB := core.ParseAddress(b)
	if errA != nil || errB != nil || a == b || a == legacy || b == legacy {
		t.Fatalf("legacy addresses were not migrated uniquely: %q (%v), %q (%v)", a, errA, b, errB)
	}
	second, err := New(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if second.snapshot().Users["first"].Address != parsedA.String() || second.snapshot().Users["second"].Address != parsedB.String() {
		t.Fatal("migrated addresses did not persist across restart")
	}
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
