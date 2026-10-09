package auth

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxAvatarBytes = 2 << 20

func normalizeAvatar(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > maxAvatarBytes {
		return nil, errors.New("头像文件需小于 2 MB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "gif") {
		return nil, errors.New("请选择 PNG、JPEG 或 GIF 图片")
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 12_000_000 {
		return nil, errors.New("图片尺寸过大，请使用不超过 4096 像素和 1200 万像素的图片")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("图片无法读取")
	}
	bounds := source.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	left := bounds.Min.X + (bounds.Dx()-side)/2
	top := bounds.Min.Y + (bounds.Dy()-side)/2
	target := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	// Bilinear resampling of a centered square; output strips metadata, animation
	// and embedded content. Only the newly encoded PNG is stored and served.
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			sx := (float64(x)+.5)*float64(side)/256 - .5
			sy := (float64(y)+.5)*float64(side)/256 - .5
			x0 := max(0, int(sx))
			y0 := max(0, int(sy))
			dx := max(0.0, sx-float64(x0))
			dy := max(0.0, sy-float64(y0))
			samples := [4]color.NRGBA{}
			samples[0] = color.NRGBAModel.Convert(source.At(left+x0, top+y0)).(color.NRGBA)
			samples[1] = color.NRGBAModel.Convert(source.At(left+min(x0+1, side-1), top+y0)).(color.NRGBA)
			samples[2] = color.NRGBAModel.Convert(source.At(left+x0, top+min(y0+1, side-1))).(color.NRGBA)
			samples[3] = color.NRGBAModel.Convert(source.At(left+min(x0+1, side-1), top+min(y0+1, side-1))).(color.NRGBA)
			blend := func(a, b, c, d uint8) uint8 {
				return uint8((1-dy)*((1-dx)*float64(a)+dx*float64(b)) + dy*((1-dx)*float64(c)+dx*float64(d)) + .5)
			}
			target.SetNRGBA(x, y, color.NRGBA{R: blend(samples[0].R, samples[1].R, samples[2].R, samples[3].R), G: blend(samples[0].G, samples[1].G, samples[2].G, samples[3].G), B: blend(samples[0].B, samples[1].B, samples[2].B, samples[3].B), A: blend(samples[0].A, samples[1].A, samples[2].A, samples[3].A)})
		}
	}
	var output bytes.Buffer
	if err = png.Encode(&output, target); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
func (s *Service) Avatar(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	var key *string
	if s.options.Avatars == nil {
		httpapi.Error(w, 404, errors.New("未设置图片头像"))
		return
	}
	if err := s.db.QueryRow(r.Context(), `SELECT avatar_object_key FROM users WHERE id=$1`, p.UserID).Scan(&key); err != nil || key == nil {
		httpapi.Error(w, 404, errors.New("未设置图片头像"))
		return
	}
	reader, err := s.options.Avatars.Get(r.Context(), *key)
	if err != nil {
		httpapi.Error(w, 503, errors.New("头像暂时无法读取"))
		return
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, (512<<10)+1))
	if err != nil || len(data) > 512<<10 {
		httpapi.Error(w, 503, errors.New("头像暂时无法读取"))
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	_, _ = w.Write(data)
}
func (s *Service) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	if s.options.Avatars == nil {
		httpapi.Error(w, 503, errors.New("头像存储尚未配置"))
		return
	}
	if !s.allowAttempt(r, "avatar", p.UserID.String(), 10) {
		httpapi.Error(w, 429, errors.New("上传过于频繁，请稍后再试"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+(64<<10))
	if err := r.ParseMultipartForm(maxAvatarBytes); err != nil {
		httpapi.Error(w, 400, errors.New("请选择小于 2 MB 的图片"))
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("avatar")
	if err != nil {
		httpapi.Error(w, 400, errors.New("请选择头像图片"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
	if err != nil {
		httpapi.Error(w, 400, errors.New("图片无法读取"))
		return
	}
	pngData, err := normalizeAvatar(data)
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	key, err := s.saveAvatar(r.Context(), p.UserID, pngData, false, sessionDigest(r))
	if err != nil {
		httpapi.Error(w, 500, errors.New("头像保存失败，请稍后重试"))
		return
	}
	p.AvatarURL = "/api/v1/me/avatar?v=" + url.QueryEscape(key)
	httpapi.JSON(w, 200, p)
}
func (s *Service) saveAvatar(ctx context.Context, userID uuid.UUID, data []byte, onlyEmpty bool, sessionHash []byte) (string, error) {
	key := "avatars/" + userID.String() + "/" + uuid.NewString() + ".png"
	if err := s.options.Avatars.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "image/png"); err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			s.deleteAvatarObject(key)
		}
	}()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if sessionHash != nil {
		if err = lockAccountSession(ctx, tx, userID, sessionHash); err != nil {
			return "", err
		}
	}
	var old *string
	var enabled bool
	if err = tx.QueryRow(ctx, `SELECT avatar_object_key,NOT disabled FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&old, &enabled); err != nil || !enabled {
		return "", errAccountUnavailable
	}
	if onlyEmpty && old != nil {
		return "", errors.New("avatar already selected")
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET avatar_object_key=$2 WHERE id=$1`, userID, key); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	committed = true
	if old != nil {
		s.deleteAvatarObject(*old)
	}
	return key, nil
}
func (s *Service) deleteAvatarObject(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.options.Avatars.Delete(ctx, key)
}
func (s *Service) RemoveAvatar(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpapi.Error(w, 500, errors.New("无法恢复默认头像"))
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockAccountSession(r.Context(), tx, p.UserID, sessionDigest(r)); err != nil {
		httpapi.Error(w, 401, errors.New("请重新登录"))
		return
	}
	var old *string
	if err = tx.QueryRow(r.Context(), `SELECT avatar_object_key FROM users WHERE id=$1`, p.UserID).Scan(&old); err != nil {
		httpapi.Error(w, 500, errors.New("无法恢复默认头像"))
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET avatar_object_key=NULL WHERE id=$1`, p.UserID); err != nil {
		httpapi.Error(w, 500, errors.New("无法恢复默认头像"))
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpapi.Error(w, 500, errors.New("无法恢复默认头像"))
		return
	}
	if old != nil && s.options.Avatars != nil {
		s.deleteAvatarObject(*old)
	}
	p.AvatarURL = ""
	httpapi.JSON(w, 200, p)
}
func (s *Service) UseProviderAvatar(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	provider := chi.URLParam(r, "provider")
	var avatar string
	if err := s.db.QueryRow(r.Context(), `SELECT avatar_url FROM auth_identities WHERE user_id=$1 AND provider=$2`, p.UserID, provider).Scan(&avatar); err != nil || avatar == "" {
		httpapi.Error(w, 400, errors.New("此登录账号暂无可用头像"))
		return
	}
	if !s.allowAttempt(r, "avatar", p.UserID.String(), 10) {
		httpapi.Error(w, 429, errors.New("操作过于频繁，请稍后再试"))
		return
	}
	if err := s.importProviderAvatar(r.Context(), p.UserID, avatar, true, sessionDigest(r)); err != nil {
		httpapi.Error(w, 503, errors.New("无法获取第三方头像，你也可以上传图片"))
		return
	}
	s.Middleware(http.HandlerFunc(s.Me)).ServeHTTP(w, r)
}
func allowedAvatarURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "avatars.githubusercontent.com", "lh3.googleusercontent.com", "lh4.googleusercontent.com", "lh5.googleusercontent.com", "lh6.googleusercontent.com":
		return true
	}
	return false
}
func (s *Service) importProviderAvatar(ctx context.Context, userID uuid.UUID, raw string, replace bool, sessionHashes ...[]byte) error {
	if s.options.Avatars == nil || !allowedAvatarURL(raw) {
		return errors.New("unsupported avatar URL")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	// No provider credentials, cookies or redirects are sent to image hosts.
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("avatar fetch failed")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxAvatarBytes+1))
	if err != nil {
		return err
	}
	normalized, err := normalizeAvatar(data)
	if err != nil {
		return err
	}
	var sessionHash []byte
	if len(sessionHashes) > 0 {
		sessionHash = sessionHashes[0]
	}
	_, err = s.saveAvatar(ctx, userID, normalized, !replace, sessionHash)
	return err
}
