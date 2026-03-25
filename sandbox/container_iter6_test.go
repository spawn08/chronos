package sandbox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDockerAPI_MockRoundTripper_OK(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{})
	sb.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet || !strings.Contains(req.URL.Path, "/v1.41/version") {
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"Version":"test"}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	resp, err := sb.dockerAPI(context.Background(), http.MethodGet, "/v1.41/version", nil)
	if err != nil {
		t.Fatalf("dockerAPI: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestDockerAPI_MockRoundTripper_DoError(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{})
	sb.client = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("transport failure")
		}),
	}
	_, err := sb.dockerAPI(context.Background(), http.MethodGet, "/v1.41/containers/json", nil)
	if err == nil {
		t.Fatal("expected error from client.Do")
	}
}

func TestDockerAPI_WithJSONBody(t *testing.T) {
	var sawBody bool
	sb := NewContainerSandbox(ContainerConfig{})
	sb.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			b, _ := io.ReadAll(req.Body)
			sawBody = len(b) > 0 && strings.Contains(string(b), `"Image"`)
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(strings.NewReader(`{"Id":"abc123"}`)),
				Request:    req,
			}, nil
		}),
	}
	resp, err := sb.dockerAPI(context.Background(), http.MethodPost, "/v1.41/containers/create", map[string]any{"Image": "alpine"})
	if err != nil {
		t.Fatalf("dockerAPI: %v", err)
	}
	resp.Body.Close()
	if !sawBody {
		t.Error("expected non-empty JSON body in request")
	}
}

func TestExecute_MockFullFlow(t *testing.T) {
	step := 0
	sb := NewContainerSandbox(ContainerConfig{})
	sb.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			step++
			path := req.URL.Path
			q := req.URL.RawQuery
			switch {
			case strings.Contains(path, "/containers/create"):
				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(strings.NewReader(`{"Id":"cid-flow"}`)),
					Request:    req,
				}, nil
			case strings.Contains(path, "/start"):
				return &http.Response{
					StatusCode: http.StatusNoContent,
					Body:       io.NopCloser(strings.NewReader("")),
					Request:    req,
				}, nil
			case strings.Contains(path, "/wait"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"StatusCode":0}`)),
					Request:    req,
				}, nil
			case strings.Contains(path, "/logs") && strings.Contains(q, "stdout=1"):
				// minimal docker multiplexed frame: 8-byte header + payload
				payload := []byte("out")
				hdr := []byte{1, 0, 0, 0, 0, 0, 0, byte(len(payload))}
				body := make([]byte, 0, len(hdr)+len(payload))
				body = append(body, hdr...)
				body = append(body, payload...)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(string(body))),
					Request:    req,
				}, nil
			case strings.Contains(path, "/logs") && strings.Contains(q, "stderr=1"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("")),
					Request:    req,
				}, nil
			case req.Method == http.MethodDelete && strings.Contains(path, "/containers/"):
				return &http.Response{
					StatusCode: http.StatusNoContent,
					Body:       io.NopCloser(strings.NewReader("")),
					Request:    req,
				}, nil
			default:
				t.Fatalf("unexpected request step %d: %s %s", step, req.Method, path)
				return nil, nil
			}
		}),
	}

	res, err := sb.Execute(context.Background(), "echo", []string{"hi"}, 30*time.Second)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, "out") {
		t.Errorf("stdout = %q, want substring out", res.Stdout)
	}
}

func TestExecute_CreateNonCreated(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{})
	sb.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/containers/create") {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Body:       io.NopCloser(strings.NewReader(`no such image`)),
					Request:    req,
				}, nil
			}
			t.Fatalf("unexpected %s", req.URL.Path)
			return nil, nil
		}),
	}
	_, err := sb.Execute(context.Background(), "true", nil, time.Second)
	if err == nil || !strings.Contains(err.Error(), "container create") {
		t.Fatalf("expected container create error, got %v", err)
	}
}

func TestExecute_CreateDecodeError(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{})
	sb.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(strings.NewReader(`not-json`)),
				Request:    req,
			}, nil
		}),
	}
	_, err := sb.Execute(context.Background(), "x", nil, time.Second)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestExecute_StartError(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{})
	sb.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/containers/create") {
				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(strings.NewReader(`{"Id":"x"}`)),
					Request:    req,
				}, nil
			}
			if strings.Contains(req.URL.Path, "/start") {
				return nil, fmt.Errorf("start failed")
			}
			if req.Method == http.MethodDelete {
				return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
		}),
	}
	_, err := sb.Execute(context.Background(), "true", nil, time.Second)
	if err == nil || !strings.Contains(err.Error(), "container start") {
		t.Fatalf("expected start error, got %v", err)
	}
}

func TestCollectLogs_StdoutAPIError(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{})
	sb.client = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("logs unavailable")
		}),
	}
	out, errOut := sb.collectLogs(context.Background(), "any")
	if out != "" || errOut != "" {
		t.Fatalf("expected empty logs on error, got %q %q", out, errOut)
	}
}

func TestCollectLogs_StderrAPIErrorAfterStdout(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{})
	sb.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.RawQuery, "stderr=0") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("plain")),
					Request:    req,
				}, nil
			}
			return nil, fmt.Errorf("stderr logs fail")
		}),
	}
	out, errOut := sb.collectLogs(context.Background(), "cid")
	if errOut != "" {
		t.Errorf("stderr = %q, want empty", errOut)
	}
	if out == "" {
		t.Error("expected some stdout from first response")
	}
}

func TestRemoveContainer_MockDelete(t *testing.T) {
	var sawDelete bool
	sb := NewContainerSandbox(ContainerConfig{})
	sb.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodDelete {
				sawDelete = true
				return &http.Response{
					StatusCode: http.StatusNoContent,
					Body:       io.NopCloser(strings.NewReader("")),
					Request:    req,
				}, nil
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
		}),
	}
	sb.removeContainer("rm-me")
	if !sawDelete {
		t.Error("expected DELETE to docker API")
	}
}

func TestRemoveContainer_DoError(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{})
	sb.client = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("network down")
		}),
	}
	// Should not panic
	sb.removeContainer("x")
}

func TestExecute_WaitError(t *testing.T) {
	sb := NewContainerSandbox(ContainerConfig{})
	sb.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(req.URL.Path, "/containers/create"):
				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(strings.NewReader(`{"Id":"w"}`)),
					Request:    req,
				}, nil
			case strings.Contains(req.URL.Path, "/start"):
				return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
			case strings.Contains(req.URL.Path, "/wait"):
				return nil, fmt.Errorf("wait failed")
			case req.Method == http.MethodDelete:
				return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
			default:
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
			}
		}),
	}
	_, err := sb.Execute(context.Background(), "true", nil, time.Second)
	if err == nil || !strings.Contains(err.Error(), "container wait") {
		t.Fatalf("expected wait error, got %v", err)
	}
}
