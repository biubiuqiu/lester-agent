package speech

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct{ Service *Service }

func speechActor(w http.ResponseWriter, r *http.Request) (Actor, bool) {
	w.Header().Set("Cache-Control", "no-store")
	a, ok := auth.FromContext(r.Context())
	if !ok {
		httpapi.Error(w, 401, ErrUnauthorized)
	}
	return a, ok
}
func (h *Handler) Config(w http.ResponseWriter, r *http.Request) {
	a, ok := speechActor(w, r)
	if !ok {
		return
	}
	c, err := h.Service.Config(r.Context(), a)
	if err != nil {
		httpapi.Error(w, errorStatus(err), err)
		return
	}
	httpapi.JSON(w, 200, c)
}
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	a, ok := speechActor(w, r)
	if !ok {
		return
	}
	var in ConfigChange
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		httpapi.Error(w, 400, ErrConfig)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		httpapi.Error(w, 400, ErrConfig)
		return
	}
	c, err := h.Service.Save(r.Context(), a, in)
	if err != nil {
		httpapi.Error(w, errorStatus(err), err)
		return
	}
	httpapi.JSON(w, 200, c)
}
func (h *Handler) Capabilities(w http.ResponseWriter, r *http.Request) {
	a, ok := speechActor(w, r)
	if !ok {
		return
	}
	c, err := h.Service.Capabilities(r.Context(), a)
	if err != nil {
		httpapi.Error(w, errorStatus(err), err)
		return
	}
	httpapi.JSON(w, 200, c)
}
func (h *Handler) Receipt(w http.ResponseWriter, r *http.Request) {
	a, ok := speechActor(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, ErrRequest)
		return
	}
	value, err := h.Service.Receipt(r.Context(), a, id)
	if err != nil {
		httpapi.Error(w, errorStatus(err), err)
		return
	}
	httpapi.JSON(w, 200, value)
}
func (h *Handler) Transcribe(w http.ResponseWriter, r *http.Request) {
	a, ok := speechActor(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
	reader, err := r.MultipartReader()
	if err != nil {
		httpapi.Error(w, 400, ErrRequest)
		return
	}
	fields := map[string]string{}
	seen := map[string]bool{}
	upload := Upload{}
	hasFile := false
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			httpapi.Error(w, 413, ErrLimit)
			return
		}
		name := part.FormName()
		if name == "file" {
			if hasFile {
				part.Close()
				httpapi.Error(w, 400, ErrRequest)
				return
			}
			hasFile = true
			upload.Data, err = io.ReadAll(io.LimitReader(part, MaxAudioBytes+1))
			part.Close()
			if err != nil || len(upload.Data) > MaxAudioBytes {
				httpapi.Error(w, 413, ErrLimit)
				return
			}
		} else {
			if name != "request_id" && name != "language" && name != "configuration_revision" || seen[name] {
				part.Close()
				httpapi.Error(w, 400, ErrRequest)
				return
			}
			seen[name] = true
			data, err := io.ReadAll(io.LimitReader(part, 257))
			part.Close()
			if err != nil || len(data) > 256 {
				httpapi.Error(w, 400, ErrRequest)
				return
			}
			fields[name] = string(data)
		}
	}
	id, err := uuid.Parse(fields["request_id"])
	if err != nil || id == uuid.Nil || id.String() != fields["request_id"] || !hasFile {
		httpapi.Error(w, 400, ErrRequest)
		return
	}
	revision, err := strconv.ParseInt(fields["configuration_revision"], 10, 64)
	if err != nil || revision < 0 {
		httpapi.Error(w, 400, ErrRequest)
		return
	}
	upload.Language = fields["language"]
	upload.ConfigurationRevision = revision
	value, err := h.Service.Transcribe(r.Context(), a, id, upload)
	if err != nil {
		httpapi.Error(w, errorStatus(err), err)
		return
	}
	status := http.StatusOK
	if value.Status == "processing" {
		status = http.StatusAccepted
	}
	httpapi.JSON(w, status, value)
}
