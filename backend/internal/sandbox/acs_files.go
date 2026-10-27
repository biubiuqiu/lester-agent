package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (r *openKruiseRuntime) Remove(ctx context.Context, target string) error {
	return r.client.Files.Remove(ctx, target)
}

func (p *ACSProvider) WriteFileVersioned(ctx context.Context, providerRef, workDir, filePath string, request FileWriteRequest) (*FileWriteResult, error) {
	if len(request.Content) > acsMaxFileBytes {
		return nil, errors.New("file exceeds the 25 MiB write limit")
	}
	input, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	output, err := p.runFileToolbox(ctx, providerRef, workDir, filePath, "write-checked", input)
	if err != nil {
		return nil, err
	}
	var result FileWriteResult
	if err = json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("decode native checked write: %w", err)
	}
	return &result, nil
}

// The same native helper owns file semantics on Docker and ACS. Direct native
// process arguments avoid user login scripts; parsing, paging, locking and
// atomic replacement stay in the versioned protocol.
func (p *ACSProvider) runFileToolbox(ctx context.Context, providerRef, workDir, filePath, operation string, input []byte, extra ...string) ([]byte, error) {
	root, target, err := scopedPath(workDir, filePath)
	if err != nil {
		return nil, err
	}
	runtimeClient, err := p.runtime(ctx, providerRef)
	if err != nil {
		return nil, err
	}
	version, err := runACSToolbox(ctx, runtimeClient, []string{"version"}, nil, nil)
	if err != nil || version == nil || version.ExitCode != 0 || strings.TrimSpace(version.Stdout) != "2" {
		return nil, errors.New("Computer file operations require lester-toolbox protocol 2; rebuild the ACS runtime template before modifying files")
	}
	arguments := []string{operation, "--root", root, "--path", target}
	if input != nil {
		if len(input) > (acsMaxFileBytes*4/3)+(1<<20) {
			return nil, errors.New("file-operation request exceeds the size limit")
		}
		staged := "/tmp/.lester-input-" + uuid.NewString() + ".json"
		// The helper unlinks the request before applying it. Also clean up when
		// transport cancellation prevents the helper from even starting.
		if remover, supported := runtimeClient.(interface {
			Remove(context.Context, string) error
		}); supported {
			defer func() {
				cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer stop()
				_ = remover.Remove(cleanup, staged)
			}()
		}
		prepared, prepareErr := runACSToolbox(ctx, runtimeClient, []string{"prepare-input", "--name", strings.TrimPrefix(staged, "/tmp/")}, nil, nil)
		if prepareErr != nil || prepared == nil || prepared.ExitCode != 0 || strings.TrimSpace(prepared.Stdout) != staged {
			return nil, errors.New("could not prepare a private native file request")
		}
		if err = runtimeClient.Write(ctx, staged, input); err != nil {
			return nil, fmt.Errorf("stage native file request: %w", err)
		}
		arguments = append(arguments, "--input-file", staged)
	}
	arguments = append(arguments, extra...)
	stdout, stderr := newBoundedCapture(commandCaptureLimit), newBoundedCapture(commandCaptureLimit)
	result, runErr := runACSToolbox(ctx, runtimeClient, arguments, func(value string) { _, _ = stdout.Write([]byte(value)) }, func(value string) { _, _ = stderr.Write([]byte(value)) })
	if result == nil {
		return nil, fmt.Errorf("native file helper did not return a result: %w", runErr)
	}
	if stdout.String() == "" {
		_, _ = stdout.Write([]byte(result.Stdout))
	}
	if stderr.String() == "" {
		_, _ = stderr.Write([]byte(result.Stderr))
	}
	if runErr != nil || result.ExitCode != 0 {
		return nil, fmt.Errorf("native file helper %s failed: %s", operation, strings.TrimSpace(stderr.String()))
	}
	if stdout.Truncated() {
		return nil, errors.New("native file helper result exceeded the bounded protocol response")
	}
	return []byte(stdout.String()), nil
}
