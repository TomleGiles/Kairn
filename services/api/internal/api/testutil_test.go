package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/kairn-io/kairn/pkg/ratelimit"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/config"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/objstore"
	"github.com/kairn-io/kairn/pkg/secrets"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/store/memstore"
	"github.com/kairn-io/kairn/pkg/store/pgstore"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb/memtsdb"
)

type fakeJobs struct{ calls []string }

func (f *fakeJobs) RequestSync(_ context.Context, org, conn string, days int) error {
	f.calls = append(f.calls, "sync:"+conn)
	return nil
}
func (f *fakeJobs) RequestRecompute(_ context.Context, org string, from, to time.Time) error {
	f.calls = append(f.calls, "recompute")
	return nil
}
func (f *fakeJobs) RequestAnalytics(_ context.Context, org string) error {
	f.calls = append(f.calls, "analytics")
	return nil
}
func (f *fakeJobs) RequestReport(_ context.Context, org, period string) error {
	f.calls = append(f.calls, "report:"+period)
	return nil
}

type testEnv struct {
	t    *testing.T
	srv  *Server
	st   store.Store
	db   *memtsdb.DB
	jobs *fakeJobs
	now  time.Time
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	kek, _ := secrets.GenerateKEK()
	w, _ := secrets.NewLocalKEK(kek)
	env := &testEnv{t: t, st: testStore(t), db: memtsdb.New(), jobs: &fakeJobs{}, now: time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)}
	env.srv = New(Deps{
		Store: env.st, TSDB: env.db, Keyring: secrets.NewKeyring(w), Jobs: env.jobs,
		Sessions: auth.SessionIssuer{Secret: []byte(strings.Repeat("s", 32))},
		Objects:  objstore.Local{Dir: t.TempDir()},
		Config:   config.Config{DevLogin: true, PublicURL: "http://localhost:3000", APIURL: "http://localhost:8080", ServiceToken: "svc"},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:      func() time.Time { return env.now },
	})
	env.srv.limiter = ratelimit.NewMemory(1e6, 1e6)
	return env
}

// testStore renvoie le store mémoire, ou PostgreSQL si KAIRN_TEST_PG_DSN est défini
// (make test-int) : toute la suite API, isolation comprise, s'exécute alors avec la RLS.
func testStore(t *testing.T) store.Store {
	dsn := os.Getenv("KAIRN_TEST_PG_DSN")
	if dsn == "" {
		return memstore.New()
	}
	st, err := pgstore.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgstore: %v", err)
	}
	t.Cleanup(st.Close)
	// Chaque test part d'une base sans organisation (les tests ne sont pas parallèles).
	orgIDs, err := st.System().ListOrgIDs(context.Background())
	if err != nil {
		t.Fatalf("list orgs: %v", err)
	}
	for pass := 0; pass < 3 && len(orgIDs) > 0; pass++ {
		var left []string
		for _, id := range orgIDs {
			if err := st.Orgs().Delete(tenancy.WithOrg(context.Background(), id), id); err != nil {
				left = append(left, id) // clientes MSP à supprimer d'abord
			}
		}
		orgIDs = left
	}
	return st
}

type resp struct {
	Code int
	Body []byte
	Hdr  http.Header
}

func (r resp) JSON(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("decode %s: %v", r.Body, err)
	}
}

// do exécute une requête avec un jeton (bearer) optionnel.
func (e *testEnv) do(method, path, token string, body any) resp {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return resp{Code: rec.Code, Body: rec.Body.Bytes(), Hdr: rec.Header()}
}

// login ouvre une session et renvoie son jeton.
func (e *testEnv) login(email string) string {
	e.t.Helper()
	r := e.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"email": email})
	if r.Code != http.StatusOK {
		e.t.Fatalf("login: %d %s", r.Code, r.Body)
	}
	var out struct {
		Token string `json:"token"`
	}
	r.JSON(e.t, &out)
	return out.Token
}

// org crée une organisation et renvoie son identifiant.
func (e *testEnv) org(token, name string) model.Organization {
	e.t.Helper()
	r := e.do(http.MethodPost, "/api/v1/orgs", token, map[string]string{"name": name})
	if r.Code != http.StatusCreated {
		e.t.Fatalf("create org: %d %s", r.Code, r.Body)
	}
	var o model.Organization
	r.JSON(e.t, &o)
	return o
}

func expect(t *testing.T, r resp, code int) {
	t.Helper()
	if r.Code != code {
		t.Fatalf("want %d, got %d: %s", code, r.Code, r.Body)
	}
}
