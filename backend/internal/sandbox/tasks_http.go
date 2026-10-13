package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/biubiuqiu/lester-agent/backend/internal/commandtask"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
)

func (c *Client) StartTask(ctx context.Context, id string, input CommandTaskRequest) (*CommandTaskSnapshot, error) {
	var result CommandTaskSnapshot
	err := c.json(ctx, "POST", "/v1/sandboxes/"+url.PathEscape(id)+"/command-tasks", input, &result)
	return &result, err
}
func (c *Client) InspectTask(ctx context.Context, id string, h CommandTaskHandle, read CommandTaskReadOptions) (*CommandTaskSnapshot, error) {
	var result CommandTaskSnapshot
	err := c.json(ctx, "POST", "/v1/sandboxes/"+url.PathEscape(id)+"/command-tasks/inspect", commandtask.InspectRequest{Handle: h, Read: read}, &result)
	return &result, err
}
func (c *Client) StopTask(ctx context.Context, id string, h CommandTaskHandle) (*CommandTaskSnapshot, error) {
	var result CommandTaskSnapshot
	err := c.json(ctx, "POST", "/v1/sandboxes/"+url.PathEscape(id)+"/command-tasks/stop", h, &result)
	return &result, err
}

func (h *ServiceHandler) taskEndpoint(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider, ok := h.provider.(ManagedCommandProvider)
		if !ok {
			httpapi.Error(w, http.StatusNotImplemented, errors.New("provider does not support managed command tasks"))
			return
		}
		decode := func(value any) bool {
			d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
			d.DisallowUnknownFields()
			if err := d.Decode(value); err != nil {
				httpapi.Error(w, 400, err)
				return false
			}
			if d.Decode(new(any)) != io.EOF {
				httpapi.Error(w, 400, errors.New("expected one task request"))
				return false
			}
			return true
		}
		id := chi.URLParam(r, "id")
		var result *CommandTaskSnapshot
		var err error
		switch operation {
		case "start":
			var input CommandTaskRequest
			if !decode(&input) {
				return
			}
			result, err = provider.StartTask(r.Context(), id, input)
		case "inspect":
			var input commandtask.InspectRequest
			if !decode(&input) {
				return
			}
			result, err = provider.InspectTask(r.Context(), id, input.Handle, input.Read)
		case "stop":
			var input CommandTaskHandle
			if !decode(&input) {
				return
			}
			result, err = provider.StopTask(r.Context(), id, input)
		}
		if err != nil {
			httpapi.Error(w, 500, err)
			return
		}
		httpapi.JSON(w, 200, result)
	}
}
