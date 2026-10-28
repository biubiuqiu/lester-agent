package speech

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var languagePattern = regexp.MustCompile(`^[a-z]{2,3}$`)

func uploadDigest(upload Upload) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(upload.MIME + "\x00" + upload.Language + "\x00" + strconv.FormatInt(upload.ConfigurationRevision, 10) + "\x00"))
	_, _ = hash.Write(upload.Data)
	return hex.EncodeToString(hash.Sum(nil))
}

// The user guard never locks account/settings rows across provider HTTP. A
// disconnect/disable therefore remains immediate, and accepting text rechecks it.
func (s *Service) guard(ctx context.Context, a Actor) (*pgx.Conn, bool, error) {
	conn, err := pgx.ConnectConfig(ctx, s.DB.Config().ConnConfig.Copy())
	if err != nil {
		return nil, false, ErrStorage
	}
	var held bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, "lester:speech:"+a.UserID.String()).Scan(&held); err != nil {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, false, ErrStorage
	}
	return conn, held, nil
}
func closeGuard(ctx context.Context, conn *pgx.Conn) {
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_ = conn.Close(finish)
}

func (s *Service) readReceipt(ctx context.Context, a Actor, id uuid.UUID, digest string) (Receipt, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Receipt{}, ErrStorage
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, a, false); err != nil {
		return Receipt{}, err
	}
	var value Receipt
	var stored string
	var expired bool
	err = tx.QueryRow(ctx, `SELECT id,status,text_content,error_message,request_digest,expires_at<=now() FROM speech_transcriptions WHERE id=$1 AND workspace_id=$2 AND user_id=$3`, id, a.WorkspaceID, a.UserID).Scan(&value.ID, &value.Status, &value.Text, &value.Error, &stored, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return Receipt{}, ErrNotFound
	}
	if err != nil {
		return Receipt{}, ErrStorage
	}
	if expired {
		return Receipt{}, ErrExpired
	}
	if digest != "" && digest != stored {
		return Receipt{}, ErrConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return Receipt{}, ErrStorage
	}
	return value, nil
}
func (s *Service) Receipt(ctx context.Context, a Actor, id uuid.UUID) (Receipt, error) {
	if id == uuid.Nil {
		return Receipt{}, ErrRequest
	}
	value, err := s.readReceipt(ctx, a, id, "")
	if err != nil || value.Status != "processing" {
		return value, err
	}
	conn, held, err := s.guard(ctx, a)
	if err != nil {
		return Receipt{}, err
	}
	defer closeGuard(ctx, conn)
	if held {
		// An absent owner means the original request was interrupted. Never run
		// its provider call again merely to recover a missing response.
		_, err = s.DB.Exec(ctx, `UPDATE speech_transcriptions SET status='unknown',error_message=$4 WHERE id=$1 AND workspace_id=$2 AND user_id=$3 AND status='processing'`, id, a.WorkspaceID, a.UserID, ErrProvider.Error())
		if err != nil {
			return Receipt{}, ErrStorage
		}
		return s.readReceipt(ctx, a, id, "")
	}
	return value, nil
}

func (s *Service) Transcribe(ctx context.Context, a Actor, id uuid.UUID, upload Upload) (Receipt, error) {
	if id == uuid.Nil || len(upload.Data) == 0 {
		return Receipt{}, ErrRequest
	}
	if len(upload.Data) > MaxAudioBytes {
		return Receipt{}, ErrLimit
	}
	upload.Language = strings.ToLower(strings.TrimSpace(upload.Language))
	if upload.Language == "auto" {
		upload.Language = ""
	}
	if upload.Language != "" && !languagePattern.MatchString(upload.Language) {
		return Receipt{}, ErrRequest
	}
	mime, extension, err := validateAudio(upload.Data)
	if err != nil {
		return Receipt{}, err
	}
	upload.MIME = mime
	upload.Filename = "recording." + extension
	digest := uploadDigest(upload)
	if existing, err := s.readReceipt(ctx, a, id, digest); err == nil {
		return s.Receipt(ctx, a, existing.ID)
	} else if !errors.Is(err, ErrNotFound) {
		return Receipt{}, err
	}
	conn, held, err := s.guard(ctx, a)
	if err != nil {
		return Receipt{}, err
	}
	defer closeGuard(ctx, conn)
	if !held {
		if existing, e := s.readReceipt(ctx, a, id, digest); e == nil {
			return existing, nil
		}
		return Receipt{}, ErrBusy
	}
	// GC clears private text after five minutes. Opaque request tombstones last
	// 24h, so an expired request cannot be mistaken for a retryable new upload.
	if _, err = s.DB.Exec(ctx, `UPDATE speech_transcriptions SET text_content='',request_digest='' WHERE expires_at<=now() AND (text_content<>'' OR request_digest<>''); DELETE FROM speech_transcriptions WHERE created_at<now()-interval '24 hours'`); err != nil {
		return Receipt{}, ErrStorage
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Receipt{}, ErrStorage
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, a, false); err != nil {
		return Receipt{}, err
	}
	var existing Receipt
	var existingDigest string
	var expired bool
	err = tx.QueryRow(ctx, `SELECT id,status,text_content,error_message,request_digest,expires_at<=now() FROM speech_transcriptions WHERE id=$1 AND workspace_id=$2 AND user_id=$3`, id, a.WorkspaceID, a.UserID).Scan(&existing.ID, &existing.Status, &existing.Text, &existing.Error, &existingDigest, &expired)
	if err == nil {
		if expired {
			return Receipt{}, ErrExpired
		}
		if digest != existingDigest {
			return Receipt{}, ErrConflict
		}
		if existing.Status == "processing" {
			existing.Status = "unknown"
			existing.Error = ErrProvider.Error()
			if _, err = tx.Exec(ctx, `UPDATE speech_transcriptions SET status='unknown',error_message=$2 WHERE id=$1`, id, existing.Error); err != nil {
				return Receipt{}, ErrStorage
			}
		}
		if err = tx.Commit(ctx); err != nil {
			return Receipt{}, ErrStorage
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Receipt{}, ErrStorage
	}
	var recent int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM speech_transcriptions WHERE user_id=$1 AND created_at>now()-interval '1 minute'`, a.UserID).Scan(&recent); err != nil {
		return Receipt{}, ErrStorage
	}
	if recent >= 12 {
		return Receipt{}, ErrRate
	}
	cfg, err := readConfig(ctx, tx, false)
	if err != nil {
		return Receipt{}, ErrStorage
	}
	if !cfg.Enabled || !cfg.HasCredential {
		return Receipt{}, ErrDisabled
	}
	if cfg.Revision != upload.ConfigurationRevision {
		return Receipt{}, ErrConflict
	}
	credential, err := s.credential(cfg)
	if err != nil {
		return Receipt{}, ErrConfig
	}
	_, err = tx.Exec(ctx, `INSERT INTO speech_transcriptions(id,workspace_id,user_id,request_digest,settings_revision,status) VALUES($1,$2,$3,$4,$5,'processing') ON CONFLICT DO NOTHING`, id, a.WorkspaceID, a.UserID, digest, cfg.Revision)
	if err != nil {
		return Receipt{}, ErrStorage
	}
	var same bool
	if err = tx.QueryRow(ctx, `SELECT workspace_id=$2 AND user_id=$3 AND request_digest=$4 FROM speech_transcriptions WHERE id=$1`, id, a.WorkspaceID, a.UserID, digest).Scan(&same); err != nil {
		return Receipt{}, ErrStorage
	}
	if !same {
		return Receipt{}, ErrConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return Receipt{}, ErrStorage
	}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	text, callErr := providerCall(callCtx, s.client, cfg, credential, upload)
	cancel()
	value := Receipt{ID: id, Status: "completed", Text: text}
	if callErr != nil {
		value.Status = "failed"
		value.Error = callErr.Error()
		value.Text = ""
		if errors.Is(callErr, ErrProvider) || ctx.Err() != nil {
			value.Status = "unknown"
			value.Error = ErrProvider.Error()
		}
	}
	finish, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	final, err := s.DB.Begin(finish)
	if err != nil {
		return Receipt{}, ErrStorage
	}
	defer final.Rollback(finish)
	if err = authorize(finish, final, a, false); err != nil {
		_ = final.Rollback(finish)
		_, _ = s.DB.Exec(finish, `UPDATE speech_transcriptions SET status='failed',text_content='',error_message=$2 WHERE id=$1`, id, ErrUnauthorized.Error())
		return Receipt{}, err
	}
	var current bool
	if err = final.QueryRow(finish, `SELECT enabled AND revision=$1 FROM speech_settings WHERE singleton=true`, cfg.Revision).Scan(&current); err != nil {
		return Receipt{}, ErrStorage
	}
	if !current {
		value.Status = "failed"
		value.Text = ""
		value.Error = ErrConflict.Error()
	}
	if _, err = final.Exec(finish, `UPDATE speech_transcriptions SET status=$2,text_content=$3,error_message=$4 WHERE id=$1`, id, value.Status, value.Text, value.Error); err != nil {
		return Receipt{}, ErrStorage
	}
	if err = final.Commit(finish); err != nil {
		return Receipt{}, ErrStorage
	}
	return value, nil
}
