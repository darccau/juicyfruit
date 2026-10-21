package memory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type OllamaClient struct {
	BaseURL        string
	Model          string
	ProbeTimeout   time.Duration
	RequestTimeout time.Duration
	Dimensions     int
}

func NewOllamaClient(baseURL, model string) *OllamaClient {
	return &OllamaClient{BaseURL: baseURL, Model: model, ProbeTimeout: 3 * time.Second, RequestTimeout: 30 * time.Second}
}

func (c *OllamaClient) resolve(path string) (string, error) {
	parsed, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	return parsed.String(), nil
}

func (c *OllamaClient) send(method, path string, timeout time.Duration, body any) (int, []byte, error) {
	endpoint, err := c.resolve(path)
	if err != nil {
		return 0, nil, err
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return response.StatusCode, nil, err
	}
	return response.StatusCode, data, nil
}

func (c *OllamaClient) Probe() bool {
	status, _, err := c.send("GET", "/api/tags", c.ProbeTimeout, nil)
	return err == nil && status == 200
}

func (c *OllamaClient) ListModels() ([]string, error) {
	status, data, err := c.send("GET", "/api/tags", c.RequestTimeout, nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, Errorf("Embedding provider returned %d for /api/tags.", status)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, Errorf("Embedding provider returned an unexpected response.")
	}
	models, _ := document["models"].([]any)
	out := []string{}
	for _, entry := range models {
		if m, ok := entry.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				out = append(out, name)
			}
		}
	}
	return out, nil
}

func (c *OllamaClient) Pull(model string, onProgress func(string)) error {
	endpoint, err := c.resolve("/api/pull")
	if err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]any{"name": model, "stream": true})
	request, err := http.NewRequest("POST", endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 0}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		_, _ = io.Copy(io.Discard, response.Body)
		return Errorf("Cannot download embedding model %s: provider returned %d.", model, response.StatusCode)
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			continue
		}
		if message, ok := decoded["error"].(string); ok {
			return Errorf("Cannot download embedding model: %s", message)
		}
		if status, ok := decoded["status"].(string); ok && onProgress != nil {
			onProgress(status)
		}
	}
	return scanner.Err()
}

func (c *OllamaClient) Embed(inputs []string) ([][]float64, error) {
	if len(inputs) == 0 {
		return [][]float64{}, nil
	}
	vectors := [][]float64{}
	for start := 0; start < len(inputs); start += 64 {
		end := start + 64
		if end > len(inputs) {
			end = len(inputs)
		}
		batch, err := c.embedBatch(inputs[start:end])
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, batch...)
	}
	if len(vectors) > 0 {
		c.Dimensions = len(vectors[0])
	}
	return vectors, nil
}

func (c *OllamaClient) embedBatch(batch []string) ([][]float64, error) {
	status, data, err := c.send("POST", "/api/embed", c.RequestTimeout, map[string]any{"model": c.Model, "input": batch})
	if err != nil {
		return nil, err
	}
	if status == 404 || status == 400 {
		return c.embedIndividually(batch)
	}
	if status != 200 {
		return nil, Errorf("Embedding provider returned %d for /api/embed.", status)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, Errorf("Embedding provider returned an unexpected response.")
	}
	embeddings, ok := document["embeddings"].([]any)
	if !ok || len(embeddings) != len(batch) {
		return nil, Errorf("Embedding provider returned no vectors.")
	}
	out := make([][]float64, 0, len(embeddings))
	for _, raw := range embeddings {
		out = append(out, asVector(raw))
	}
	return out, nil
}

func (c *OllamaClient) embedIndividually(batch []string) ([][]float64, error) {
	vectors := [][]float64{}
	for _, input := range batch {
		status, data, err := c.send("POST", "/api/embeddings", c.RequestTimeout, map[string]any{"model": c.Model, "prompt": input})
		if err != nil {
			return nil, err
		}
		if status != 200 {
			return nil, Errorf("Embedding provider returned %d for /api/embeddings.", status)
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			return nil, Errorf("Embedding provider returned an unexpected response.")
		}
		vectors = append(vectors, asVector(document["embedding"]))
	}
	return vectors, nil
}

func asVector(value any) []float64 {
	list, ok := value.([]any)
	if !ok {
		return []float64{}
	}
	out := make([]float64, 0, len(list))
	for _, item := range list {
		if number, ok := item.(float64); ok {
			out = append(out, number)
		} else {
			out = append(out, 0)
		}
	}
	return out
}
