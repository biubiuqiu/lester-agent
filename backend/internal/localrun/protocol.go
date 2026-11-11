package localrun

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"unicode/utf8"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

const MaxRPCFrame = 8 << 20

type rpcFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

// Server speaks private newline-delimited JSON-RPC. stdout is protocol only;
// errors contain no provider output, credential or private configuration.
type Server struct {
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	writeMu   sync.Mutex
	writer    io.Writer
	runner    *Runner
	factory   ModelFactory
	requests  map[string]bool
	remote    map[string]*remoteCall
	inference map[string]context.CancelFunc
	wg        sync.WaitGroup
	closed    bool
}

func NewServer(writer io.Writer, factory ModelFactory) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{writer: writer, factory: factory, ctx: ctx, cancel: cancel, requests: map[string]bool{}, remote: map[string]*remoteCall{}, inference: map[string]context.CancelFunc{}}
}

func (s *Server) write(value any) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw)+1 > MaxRPCFrame {
		return errors.New("RPC output exceeds protocol limit")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.writer.Write(append(raw, '\n'))
	if err != nil {
		s.cancel()
	}
	return err
}
func (s *Server) notify(method string, params any) {
	if err := s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params}); err != nil {
		s.cancel()
	}
}
func (s *Server) respond(id json.RawMessage, result any, requestErr error) {
	if requestErr != nil {
		_ = s.write(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32000, "message": "Local operation failed"}})
		return
	}
	if err := s.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result}); err != nil {
		_ = s.write(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32000, "message": "Local result exceeds protocol limit"}})
	}
}

func readRPCLine(reader *bufio.Reader) ([]byte, error) {
	var frame []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(frame)+len(part) > MaxRPCFrame {
			return nil, errors.New("RPC frame exceeds protocol limit")
		}
		frame = append(frame, part...)
		if err == nil {
			return bytes.TrimSuffix(frame, []byte{'\n'}), nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && len(frame) > 0 {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
}

func validRPCID(raw json.RawMessage) bool {
	if len(raw) == 0 || len(raw) > 192 {
		return false
	}
	var id any
	if json.Unmarshal(raw, &id) != nil {
		return false
	}
	switch value := id.(type) {
	case string:
		return value != "" && len(value) <= 160
	case float64:
		return true
	default:
		return false
	}
}

func (s *Server) Serve(reader io.Reader) error {
	defer s.Close()
	buffer := bufio.NewReaderSize(reader, 64<<10)
	for {
		line, err := readRPCLine(buffer)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.New("invalid RPC frame")
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var frame rpcFrame
		if !utf8.Valid(line) || json.Unmarshal(line, &frame) != nil || frame.JSONRPC != "2.0" {
			_ = s.write(map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32700, "message": "Invalid JSON-RPC frame"}})
			continue
		}
		if frame.Method == "" {
			s.receiveModelResponse(frame)
			continue
		}
		if len(frame.ID) == 0 {
			switch frame.Method {
			case "cloud.model.event":
				s.receiveCloudEvent(frame.Params)
			case "models.cancel":
				s.cancelInference(frame.Params)
			}
			continue
		}
		if !validRPCID(frame.ID) {
			s.respond(json.RawMessage("null"), nil, errors.New("invalid request identity"))
			continue
		}
		key := string(frame.ID)
		s.mu.Lock()
		if s.requests[key] || len(s.requests) >= 64 || s.closed {
			s.mu.Unlock()
			s.respond(frame.ID, nil, errors.New("RPC request limit reached"))
			continue
		}
		s.requests[key] = true
		s.wg.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.wg.Done()
			defer func() { s.mu.Lock(); delete(s.requests, key); s.mu.Unlock() }()
			result, err := s.dispatch(s.ctx, frame.Method, frame.Params)
			s.respond(frame.ID, result, err)
		}()
	}
}

func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.cancel()
	for _, call := range s.remote {
		call.finish(context.Canceled)
	}
	s.mu.Unlock()
	s.wg.Wait()
	s.mu.Lock()
	runner := s.runner
	s.mu.Unlock()
	if runner != nil {
		_ = runner.Close()
	}
}

func (s *Server) cancelInference(raw json.RawMessage) {
	var p struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	s.mu.Lock()
	cancel := s.inference[p.ID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Server) dispatch(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if !json.Valid(raw) || len(raw) == 0 || raw[0] != '{' {
		return nil, errors.New("invalid RPC parameters")
	}
	if method == "initialize" {
		var p struct {
			StateDir  string `json:"state_dir"`
			ProfileID string `json:"profile_id"`
			DeviceID  string `json:"device_id"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid initialization")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.runner != nil || s.closed {
			return nil, errors.New("runner already initialized")
		}
		store, err := OpenStore(p.StateDir, p.ProfileID, p.DeviceID)
		if err != nil {
			return nil, err
		}
		factory := func(ctx context.Context, m ModelConfig) (modelruntime.Client, error) {
			if m.Source == "cloud" {
				return &cloudClient{server: s, modelID: m.ID}, nil
			}
			if s.factory != nil {
				return s.factory(ctx, m)
			}
			return DefaultModelFactory(ctx, m)
		}
		s.runner = NewRunner(store, factory, s.notify)
		return map[string]string{"device_id": p.DeviceID, "profile_id": p.ProfileID, "execution_target": "local"}, nil
	}
	s.mu.Lock()
	r := s.runner
	s.mu.Unlock()
	if r == nil {
		return nil, errors.New("runner must be initialized")
	}
	switch method {
	case "configureModels":
		var p struct {
			Models []ModelConfig `json:"models"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid models")
		}
		return r.ConfigureModels(p.Models)
	case "models.list":
		r.mu.Lock()
		defer r.mu.Unlock()
		info := []ModelInfo{}
		for _, m := range r.models {
			info = append(info, ModelInfo{ID: m.ID, Name: m.Name, Source: m.Source, Provider: m.Provider, ModelID: m.ModelID})
		}
		return info, nil
	case "models.generate", "models.stream":
		return s.inferLocal(ctx, r, method == "models.stream", raw)
	case "models.cancel":
		s.cancelInference(raw)
		return map[string]bool{"ok": true}, nil
	case "project.open":
		var p struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Path string `json:"path"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid project")
		}
		return r.Store.OpenProject(ctx, p.ID, p.Name, p.Path)
	case "project.list":
		return r.Store.Projects(ctx)
	case "conversation.list":
		return r.Store.Conversations(ctx)
	case "conversation.create":
		var p struct {
			ProjectID string `json:"project_id"`
			ModelID   string `json:"model_id"`
			Title     string `json:"title"`
			Mode      string `json:"permission_mode"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid conversation")
		}
		if p.Mode == "" {
			p.Mode = "standard"
		}
		r.mu.Lock()
		_, ok := r.models[p.ModelID]
		r.mu.Unlock()
		if !ok {
			return nil, errors.New("selected model is unavailable")
		}
		return r.Store.CreateConversation(ctx, p.ProjectID, p.ModelID, p.Title, p.Mode)
	case "conversation.get":
		var p struct {
			ID string `json:"id"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid conversation")
		}
		return r.Store.Snapshot(ctx, p.ID)
	case "conversation.send":
		var p struct {
			ConversationID string `json:"conversation_id"`
			RequestID      string `json:"request_id"`
			Content        string `json:"content"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid submission")
		}
		run, accepted, err := r.Send(ctx, p.ConversationID, p.RequestID, p.Content)
		return map[string]any{"run": run, "accepted": accepted}, err
	case "conversation.stop":
		var p struct {
			ConversationID string `json:"conversation_id"`
			RunID          string `json:"run_id"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid stop")
		}
		stopped, err := r.Stop(ctx, p.ConversationID, p.RunID)
		return map[string]bool{"stopped": stopped}, err
	case "conversation.receipt":
		var p struct {
			RequestID string `json:"request_id"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid receipt")
		}
		return r.Store.Receipt(ctx, p.RequestID)
	case "conversation.events":
		var p struct {
			ConversationID string `json:"conversation_id"`
			After          int64  `json:"after"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid event cursor")
		}
		return r.Store.Events(ctx, p.ConversationID, p.After)
	case "permission.respond":
		var p struct {
			ConversationID string `json:"conversation_id"`
			RunID          string `json:"run_id"`
			ID             string `json:"permission_id"`
			Decision       string `json:"decision"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid permission response")
		}
		err := r.RespondPermission(ctx, p.ConversationID, p.RunID, p.ID, p.Decision)
		return map[string]bool{"ok": err == nil}, err
	case "files.list", "files.read", "git.diff":
		var p struct {
			ProjectID string `json:"project_id"`
			Path      string `json:"path"`
			Cursor    string `json:"cursor"`
			Limit     int    `json:"limit"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid file request")
		}
		project, err := r.Store.Project(ctx, p.ProjectID)
		if err != nil {
			return nil, err
		}
		fs, err := NewProjectFS(project.Path)
		if err != nil {
			return nil, err
		}
		defer fs.Close()
		if method == "files.read" {
			return fs.Read(p.Path)
		}
		if method == "git.diff" {
			diff, err := fs.GitDiff(ctx)
			return map[string]string{"diff": diff}, err
		}
		if p.Limit == 0 {
			p.Limit = 500
		}
		return fs.ListPage(p.Path, p.Cursor, p.Limit)
	case "terminal.open":
		var p struct {
			ProjectID string `json:"project_id"`
			Cols      uint16 `json:"cols"`
			Rows      uint16 `json:"rows"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid terminal request")
		}
		project, err := r.Store.Project(ctx, p.ProjectID)
		if err != nil {
			return nil, err
		}
		var id string
		ready := make(chan struct{})
		var pending []byte
		id, err = r.Terminals.Open(r.ctx, project.Path, p.Cols, p.Rows, func(data []byte) {
			<-ready
			pending = append(pending, data...)
			end := 0
			for end < len(pending) && utf8.FullRune(pending[end:]) {
				_, size := utf8.DecodeRune(pending[end:])
				end += size
			}
			if end > 0 {
				s.notify("terminal.data", map[string]string{"id": id, "data": string(pending[:end])})
				pending = append(pending[:0], pending[end:]...)
			}
		}, func(code int) { <-ready; s.notify("terminal.exit", map[string]any{"id": id, "exit_code": code}) })
		close(ready)
		return map[string]string{"id": id}, err
	case "terminal.input":
		var p struct {
			ID   string `json:"id"`
			Data string `json:"data"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid terminal input")
		}
		err := r.Terminals.Input(p.ID, []byte(p.Data))
		return map[string]bool{"ok": err == nil}, err
	case "terminal.resize":
		var p struct {
			ID   string `json:"id"`
			Cols uint16 `json:"cols"`
			Rows uint16 `json:"rows"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid terminal resize")
		}
		err := r.Terminals.Resize(p.ID, p.Cols, p.Rows)
		return map[string]bool{"ok": err == nil}, err
	case "terminal.close":
		var p struct {
			ID string `json:"id"`
		}
		if decodeStrict(raw, &p) != nil {
			return nil, errors.New("invalid terminal close")
		}
		err := r.Terminals.Close(p.ID)
		return map[string]bool{"ok": err == nil}, err
	default:
		return nil, fmt.Errorf("unknown local method")
	}
}
