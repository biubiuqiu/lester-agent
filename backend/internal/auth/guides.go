package auth

import (
	"errors"
	"net/http"

	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
)

type guideProgress struct {
	Topic  string `json:"topic"`
	Step   int    `json:"step"`
	Status string `json:"status"`
}

func validGuide(topic string) bool {
	switch topic {
	case "welcome", "models", "projects", "files", "agents", "contexts", "computer", "skills", "profile", "publishing":
		return true
	}
	return false
}

func (s *Service) Guides(w http.ResponseWriter, r *http.Request) {
	p, ok := FromContext(r.Context())
	if !ok {
		httpapi.Error(w, 401, errors.New("请重新登录"))
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT topic,step,status FROM user_guides WHERE user_id=$1 ORDER BY topic`, p.UserID)
	if err != nil {
		httpapi.Error(w, 500, errors.New("引导进度加载失败"))
		return
	}
	defer rows.Close()
	guides := []guideProgress{}
	for rows.Next() {
		var item guideProgress
		if err = rows.Scan(&item.Topic, &item.Step, &item.Status); err != nil {
			httpapi.Error(w, 500, errors.New("引导进度加载失败"))
			return
		}
		guides = append(guides, item)
	}
	if rows.Err() != nil {
		httpapi.Error(w, 500, errors.New("引导进度加载失败"))
		return
	}
	httpapi.JSON(w, 200, map[string]any{"guides": guides})
}

func (s *Service) SaveGuide(w http.ResponseWriter, r *http.Request) {
	p, ok := FromContext(r.Context())
	if !ok {
		httpapi.Error(w, 401, errors.New("请重新登录"))
		return
	}
	var req struct {
		Step   *int   `json:"step"`
		Status string `json:"status"`
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	topic := chi.URLParam(r, "topic")
	if !validGuide(topic) || req.Step == nil || *req.Step < 0 || *req.Step > 19 || (req.Status != "in_progress" && req.Status != "skipped" && req.Status != "completed") {
		httpapi.Error(w, 400, errors.New("引导进度无效"))
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpapi.Error(w, 500, errors.New("引导进度保存失败，请重试"))
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockAccountSession(r.Context(), tx, p.UserID, sessionDigest(r)); err != nil {
		httpapi.Error(w, 401, errors.New("请重新登录"))
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO user_guides(user_id,topic,step,status) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,topic) DO UPDATE SET step=EXCLUDED.step,status=EXCLUDED.status,updated_at=now()`, p.UserID, topic, *req.Step, req.Status)
	if err != nil {
		httpapi.Error(w, 500, errors.New("引导进度保存失败，请重试"))
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpapi.Error(w, 500, errors.New("引导进度保存失败，请重试"))
		return
	}
	httpapi.JSON(w, 200, guideProgress{Topic: topic, Step: *req.Step, Status: req.Status})
}
