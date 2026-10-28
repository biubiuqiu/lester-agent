package speech

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type serviceFixture struct {
	s             *Service
	admin, member Actor
	access        string
	router        http.Handler
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	dsn := os.Getenv("LESTER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LESTER_TEST_DATABASE_URL for real speech PostgreSQL tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `BEGIN;SELECT pg_advisory_xact_lock(hashtextextended('lester:test:pgcrypto',0));CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;COMMIT;`); err != nil {
		t.Fatal(err)
	}
	schema := "speech_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.MaxConns = 3
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
	s, err := New(db, []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	access := sha256.Sum256([]byte(uuid.NewString()))
	f := &serviceFixture{s: s, access: base64.RawURLEncoding.EncodeToString(access[:])}
	for index, role := range []string{"admin", "member"} {
		sum := sha256.Sum256([]byte(uuid.NewString()))
		actor := Actor{UserID: uuid.New(), WorkspaceID: uuid.New(), SessionHash: sum[:], Role: role}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO users(id,email,display_name,password_hash,role) VALUES($1,$2,'Speech user','unused',$3)`, []any{actor.UserID, fmt.Sprintf("speech-%d@example.test", index), role}},
			{`INSERT INTO workspaces(id,name) VALUES($1,'Speech workspace')`, []any{actor.WorkspaceID}},
			{`INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, []any{actor.WorkspaceID, actor.UserID}},
			{`INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 day')`, []any{actor.UserID, actor.SessionHash}},
		} {
			if _, err = db.Exec(ctx, q.sql, q.args...); err != nil {
				t.Fatal(err)
			}
		}
		if role == "admin" {
			f.admin = actor
		} else {
			f.member = actor
		}
	}
	request := httptest.NewRequest("GET", "http://127.0.0.1:13020", nil)
	request.AddCookie(&http.Cookie{Name: "lester_access_token", Value: f.access})
	if _, err = db.Exec(ctx, `INSERT INTO auth_access_tokens(token_hash,session_id,expires_at) SELECT $1,id,now()+interval '1 hour' FROM sessions WHERE token_hash=$2`, auth.AccessCredentialHash(request), f.admin.SessionHash); err != nil {
		t.Fatal(err)
	}
	h := &Handler{Service: s}
	router := chi.NewRouter()
	router.Use(auth.MutationOrigin("http://127.0.0.1:13020"))
	authService := auth.New(db, nil, 2*time.Hour, false)
	router.Group(func(r chi.Router) {
		r.Use(authService.Middleware)
		r.Get("/api/v1/speech/capabilities", h.Capabilities)
		r.Post("/api/v1/speech/transcriptions", h.Transcribe)
		r.Get("/api/v1/speech/transcriptions/{id}", h.Receipt)
		r.Group(func(a chi.Router) {
			a.Use(auth.RequireAdmin)
			a.Get("/api/v1/admin/speech", h.Config)
			a.Patch("/api/v1/admin/speech", h.Save)
		})
	})
	f.router = router
	return f
}

func tinyWAV() []byte {
	data := make([]byte, 48)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], 40)
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 16000)
	binary.LittleEndian.PutUint32(data[28:], 32000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 4)
	data[44] = 1
	return data
}
func (f *serviceFixture) enable(t *testing.T, server *httptest.Server) Config {
	t.Helper()
	c, err := f.s.Save(context.Background(), f.admin, ConfigChange{Provider: "openai", Endpoint: server.URL + "/v1", ModelID: "gpt-4o-transcribe", Credential: "private-speech-token", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func (f *serviceFixture) upload(t *testing.T, id uuid.UUID, revision int64, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("request_id", id.String())
	_ = form.WriteField("configuration_revision", fmt.Sprint(revision))
	file, err := form.CreateFormFile("file", "untrusted.webm")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write(data)
	_ = form.Close()
	request := httptest.NewRequest("POST", "/api/v1/speech/transcriptions", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("Origin", "http://127.0.0.1:13020")
	request.AddCookie(&http.Cookie{Name: "lester_access_token", Value: f.access})
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	return response
}

func TestSpeechConfigIsAdminOnlyEncryptedRevisionedAndPreservesBlankKey(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	cap, err := f.s.Capabilities(ctx, f.member)
	if err != nil || cap.Enabled || cap.MaxAudioBytes != MaxAudioBytes {
		t.Fatal(cap, err)
	}
	if _, err = f.s.Config(ctx, f.member); !errors.Is(err, ErrAdmin) {
		t.Fatal("member read adminconfig", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	c := f.enable(t, server)
	raw, _ := json.Marshal(c)
	if bytes.Contains(raw, []byte("private-speech-token")) || !c.HasCredential || c.Revision != 1 {
		t.Fatal("credential leaked or notconfigured", string(raw))
	}
	var stored []byte
	if err = f.s.DB.QueryRow(ctx, `SELECT credential_ciphertext FROM speech_settings`).Scan(&stored); err != nil || bytes.Contains(stored, []byte("private-speech-token")) {
		t.Fatal("plaintextcredential", err)
	}
	next, err := f.s.Save(ctx, f.admin, ConfigChange{Revision: c.Revision, Enabled: true, Provider: c.Provider, Endpoint: c.Endpoint, ModelID: c.ModelID})
	if err != nil || !next.HasCredential {
		t.Fatal("blankkeyfailedtopreserve", next, err)
	}
	if _, err = f.s.Save(ctx, f.admin, ConfigChange{Revision: c.Revision}); !errors.Is(err, ErrConflict) {
		t.Fatal("staleconfigaccepted", err)
	}
	if _, err = f.s.Save(ctx, f.member, ConfigChange{Revision: next.Revision}); !errors.Is(err, ErrAdmin) {
		t.Fatal("memberchangedconfig", err)
	}
	changed, err := f.s.Save(ctx, f.admin, ConfigChange{Revision: next.Revision, Provider: "elevenlabs", Enabled: false})
	if err != nil || changed.HasCredential || changed.ModelID != "scribe_v2" {
		t.Fatal("providerchangereusedoldkey", changed, err)
	}
	if _, err = f.s.Save(ctx, f.admin, ConfigChange{Revision: changed.Revision, Provider: "elevenlabs", Enabled: true}); !errors.Is(err, ErrConfig) {
		t.Fatal("enabledwithoutkey", err)
	}
	if _, err = f.s.DB.Exec(ctx, `UPDATE users SET role='member' WHERE id=$1`, f.admin.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Save(ctx, f.admin, ConfigChange{Revision: changed.Revision}); !errors.Is(err, ErrAdmin) {
		t.Fatal("staledemotionauthorized", err)
	}
}

func TestSpeechRealMultipartCompletesRecoversAndNeverRebills(t *testing.T) {
	f := newServiceFixture(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer private-speech-token" {
			t.Error("wrongprotocol")
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			t.Error(err)
		}
		if r.FormValue("model") != "gpt-4o-transcribe" {
			t.Error("wrongmodel")
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if !bytes.Equal(data, tinyWAV()) || header.Filename != "recording.wav" {
			t.Error("wrongaudio")
		}
		_, _ = io.WriteString(w, `{"text":"修复 Lester 的 GitHub 登录"}`)
	}))
	defer server.Close()
	c := f.enable(t, server)
	id := uuid.New()
	response := f.upload(t, id, c.Revision, tinyWAV())
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var value Receipt
	if json.Unmarshal(response.Body.Bytes(), &value) != nil || value.Status != "completed" || value.Text != "修复 Lester 的 GitHub 登录" {
		t.Fatal(response.Body.String())
	}
	if response = f.upload(t, id, c.Revision, tinyWAV()); response.Code != 200 || calls.Load() != 1 {
		t.Fatal("retryrebilled", response.Code, calls.Load())
	}
	if _, err := f.s.Receipt(context.Background(), f.member, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("anotheractorreadtext", err)
	}
	data := tinyWAV()
	data[44] = 9
	if response = f.upload(t, id, c.Revision, data); response.Code != 409 || calls.Load() != 1 {
		t.Fatal("changeduploadreusedid", response.Code, calls.Load())
	}
	if response = f.upload(t, uuid.New(), c.Revision+1, tinyWAV()); response.Code != 409 || calls.Load() != 1 {
		t.Fatal("newconfigdisclosurebypassed", response.Code, calls.Load())
	}
}

func TestSpeechInflightConcurrencyAndDisableDoNotBlockOnProvider(t *testing.T) {
	f := newServiceFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)
		<-release
		_, _ = io.WriteString(w, `{"text":"late text"}`)
	}))
	defer server.Close()
	c := f.enable(t, server)
	id := uuid.New()
	finished := make(chan Receipt, 1)
	fail := make(chan error, 1)
	go func() {
		result, err := f.s.Transcribe(context.Background(), f.admin, id, Upload{Data: tinyWAV(), ConfigurationRevision: c.Revision})
		finished <- result
		fail <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("providerdidnotstart")
	}
	duplicate, err := f.s.Transcribe(context.Background(), f.admin, id, Upload{Data: tinyWAV(), ConfigurationRevision: c.Revision})
	if err != nil || duplicate.Status != "processing" {
		t.Fatal("duplicatewasnotreadonly", duplicate, err)
	}
	if _, err = f.s.Transcribe(context.Background(), f.admin, uuid.New(), Upload{Data: tinyWAV(), ConfigurationRevision: c.Revision}); !errors.Is(err, ErrBusy) {
		t.Fatal("parallelcostwasallowed", err)
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	_, err = f.s.Save(ctx, f.admin, ConfigChange{Revision: c.Revision, Provider: c.Provider, Endpoint: c.Endpoint, ModelID: c.ModelID, Enabled: false})
	if err != nil {
		close(release)
		t.Fatal("disableblockedbyprovider", err)
	}
	close(release)
	result := <-finished
	if err = <-fail; err != nil || result.Status != "failed" || result.Text != "" || result.Error != ErrConflict.Error() {
		t.Fatal("stalesettingsacceptedtext", result, err)
	}
	if calls.Load() != 1 {
		t.Fatal("duplicateproviderwrite", calls.Load())
	}
}

func TestSpeechUnknownLostResponseAndAbandonedOwnerNeverReplay(t *testing.T) {
	f := newServiceFixture(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	c := f.enable(t, server)
	id := uuid.New()
	upload := Upload{Data: tinyWAV(), ConfigurationRevision: c.Revision}
	value, err := f.s.Transcribe(context.Background(), f.admin, id, upload)
	if err != nil || value.Status != "unknown" {
		t.Fatal(value, err)
	}
	if value, err = f.s.Transcribe(context.Background(), f.admin, id, upload); err != nil || value.Status != "unknown" || calls.Load() != 1 {
		t.Fatal("lostresponsereplayed", value, err, calls.Load())
	}
	abandoned := uuid.New()
	if _, err = f.s.DB.Exec(context.Background(), `INSERT INTO speech_transcriptions(id,workspace_id,user_id,request_digest,settings_revision,status) VALUES($1,$2,$3,'abandoned',$4,'processing')`, abandoned, f.admin.WorkspaceID, f.admin.UserID, c.Revision); err != nil {
		t.Fatal(err)
	}
	if value, err = f.s.Receipt(context.Background(), f.admin, abandoned); err != nil || value.Status != "unknown" || calls.Load() != 1 {
		t.Fatal("crashreplayed", value, err)
	}
}

func TestSpeechRevokedActorCannotAcceptLateTranscription(t *testing.T) {
	for _, revoke := range []string{"membership", "session", "disabled"} {
		t.Run(revoke, func(t *testing.T) {
			f := newServiceFixture(t)
			started, release := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-release
				_, _ = io.WriteString(w, `{"text":"must not be accepted"}`)
			}))
			defer server.Close()
			c := f.enable(t, server)
			id := uuid.New()
			finished := make(chan error, 1)
			go func() {
				_, err := f.s.Transcribe(context.Background(), f.admin, id, Upload{Data: tinyWAV(), ConfigurationRevision: c.Revision})
				finished <- err
			}()
			<-started
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var err error
			switch revoke {
			case "membership":
				_, err = f.s.DB.Exec(ctx, `DELETE FROM workspace_members WHERE user_id=$1`, f.admin.UserID)
			case "session":
				_, err = f.s.DB.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, f.admin.SessionHash)
			case "disabled":
				_, err = f.s.DB.Exec(ctx, `UPDATE users SET disabled=true WHERE id=$1`, f.admin.UserID)
			}
			close(release)
			if err != nil {
				t.Fatal("revocation blocked on provider", err)
			}
			if err = <-finished; !errors.Is(err, ErrUnauthorized) {
				t.Fatal("revoked actor accepted result", err)
			}
			var stored string
			err = f.s.DB.QueryRow(context.Background(), `SELECT COALESCE(max(text_content),'') FROM speech_transcriptions WHERE id=$1`, id).Scan(&stored)
			if err != nil || stored != "" {
				t.Fatal("revoked result persisted", stored, err)
			}
		})
	}
}

func TestSpeechReceiptExpiryRateAndSessionRevocation(t *testing.T) {
	f := newServiceFixture(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"text":"okay"}`)
	}))
	defer server.Close()
	c := f.enable(t, server)
	ctx := context.Background()
	expired := uuid.New()
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO speech_transcriptions(id,workspace_id,user_id,request_digest,settings_revision,status,text_content,expires_at) VALUES($1,$2,$3,'expired',$4,'completed','private expired',now()-interval '1 second')`, expired, f.admin.WorkspaceID, f.admin.UserID, c.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Receipt(ctx, f.admin, expired); !errors.Is(err, ErrExpired) {
		t.Fatal("expiredtextreturned", err)
	}
	if _, err := f.s.Transcribe(ctx, f.admin, expired, Upload{Data: tinyWAV(), ConfigurationRevision: c.Revision}); !errors.Is(err, ErrExpired) || calls.Load() != 0 {
		t.Fatal("expiredrequestrebilled", err)
	}
	if _, err := f.s.Transcribe(ctx, f.admin, uuid.New(), Upload{Data: tinyWAV(), ConfigurationRevision: c.Revision}); err != nil {
		t.Fatal(err)
	}
	var retained string
	if err := f.s.DB.QueryRow(ctx, `SELECT text_content FROM speech_transcriptions WHERE id=$1`, expired).Scan(&retained); err != nil || retained != "" {
		t.Fatal("expiredprivatebodyretained", retained, err)
	}
	for i := 0; i < 10; i++ {
		if _, err := f.s.DB.Exec(ctx, `INSERT INTO speech_transcriptions(id,workspace_id,user_id,request_digest,settings_revision,status) VALUES($1,$2,$3,'rate',$4,'completed')`, uuid.New(), f.admin.WorkspaceID, f.admin.UserID, c.Revision); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.s.Transcribe(ctx, f.admin, uuid.New(), Upload{Data: tinyWAV(), ConfigurationRevision: c.Revision}); !errors.Is(err, ErrRate) {
		t.Fatal("rateunbounded", err)
	}
	if _, err := f.s.DB.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, f.admin.SessionHash); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Transcribe(ctx, f.admin, uuid.New(), Upload{Data: tinyWAV(), ConfigurationRevision: c.Revision}); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revokedsessioncalledprovider", err)
	}
	if calls.Load() != 1 {
		t.Fatal("unexpectedprovidercalls", calls.Load())
	}
}

func TestSpeechRejectsBadFormatOversizedBodyAndForeignOrigin(t *testing.T) {
	f := newServiceFixture(t)
	if result := f.upload(t, uuid.New(), 0, []byte("<html>fakeaudio</html>")); result.Code != 400 {
		t.Fatal(result.Code, result.Body.String())
	}
	if result := f.upload(t, uuid.New(), 0, make([]byte, MaxAudioBytes+1)); result.Code != 413 {
		t.Fatal(result.Code, result.Body.String())
	}
	r := httptest.NewRequest("POST", "/api/v1/speech/transcriptions", strings.NewReader("irrelevant"))
	r.Header.Set("Origin", "https://foreign.example")
	r.AddCookie(&http.Cookie{Name: "lester_access_token", Value: f.access})
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, r)
	if response.Code != 403 {
		t.Fatal("foreignoriginaccepted", response.Code)
	}
}

func TestSpeechNormalizeEndpointsRejectsCredentialLeakVectors(t *testing.T) {
	for _, endpoint := range []string{"https://user:password@provider.test/v1", "https://provider.test/v1?secret=x", "https://provider.test/v1#x", "http://169.254.169.254/v1", "http://provider.test/v1", "https://provider.test/a/../v1", "https://provider.test/%2e%2e/v1"} {
		if _, err := normalizeConfig(ConfigChange{Endpoint: endpoint}); err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
	if _, err := normalizeConfig(ConfigChange{Provider: "elevenlabs", Endpoint: "https://foreign.example/v1"}); err == nil {
		t.Fatal("arbitraryelevenhostaccepted")
	}
	if _, err := normalizeConfig(ConfigChange{Endpoint: "http://localhost:8000/v1"}); err != nil {
		t.Fatal(err)
	}
}
