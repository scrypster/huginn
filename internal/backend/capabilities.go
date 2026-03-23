package backend

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type ModelCapabilities struct {
	ContextLength  int
	SupportsVision bool
}

type ollamaShowResponse struct {
	Details struct {
		Family   string   `json:"family"`
		Families []string `json:"families"`
	} `json:"details"`
	ModelInfo map[string]json.RawMessage `json:"model_info"`
}

func DetectVision(baseURL, model string) (bool, error) {
	reqBody, _ := json.Marshal(map[string]string{"name": model})
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(baseURL+"/api/show", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return false, nil // graceful
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	var show ollamaShowResponse
	if err := json.NewDecoder(resp.Body).Decode(&show); err != nil {
		return false, nil
	}
	visionFamilies := []string{"llava", "clip", "moondream", "bakllava"}
	checkFamily := func(s string) bool {
		lower := strings.ToLower(s)
		for _, kw := range visionFamilies {
			if strings.Contains(lower, kw) {
				return true
			}
		}
		return false
	}
	if checkFamily(show.Details.Family) {
		return true, nil
	}
	for _, f := range show.Details.Families {
		if checkFamily(f) {
			return true, nil
		}
	}
	for k, v := range show.ModelInfo {
		if strings.Contains(strings.ToLower(k), "projector") {
			return true, nil
		}
		var s string
		if json.Unmarshal(v, &s) == nil && strings.Contains(strings.ToLower(s), "projector") {
			return true, nil
		}
	}
	return false, nil
}

func FetchCapabilities(baseURL, model string) ModelCapabilities {
	supportsVision, _ := DetectVision(baseURL, model)
	return ModelCapabilities{SupportsVision: supportsVision}
}
