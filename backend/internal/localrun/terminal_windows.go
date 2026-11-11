package localrun

import "context"

type TerminalManager struct{}

func NewTerminalManager() *TerminalManager { return &TerminalManager{} }

func (*TerminalManager) Open(context.Context, string, uint16, uint16, func([]byte), func(int)) (string, error) {
	return "", ErrUnsupported
}

func (*TerminalManager) Input(string, []byte) error          { return ErrUnsupported }
func (*TerminalManager) Resize(string, uint16, uint16) error { return ErrUnsupported }
func (*TerminalManager) Close(string) error                  { return ErrUnsupported }
func (*TerminalManager) CloseAll()                           {}
