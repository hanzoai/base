package functions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenFaaSFunction represents a function in OpenFaaS.
type OpenFaaSFunction struct {
	Name              string            `json:"name"`
	Image             string            `json:"image"`
	Namespace         string            `json:"namespace,omitempty"`
	EnvProcess        string            `json:"envProcess,omitempty"`
	EnvVars           map[string]string `json:"envVars,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
	Replicas          int               `json:"replicas,omitempty"`
	InvocationCount   int               `json:"invocationCount,omitempty"`
	AvailableReplicas int               `json:"availableReplicas,omitempty"`
}

// DeployRequest is the request body for deploying a function.
type DeployRequest struct {
	Service     string            `json:"service"`
	Image       string            `json:"image"`
	Namespace   string            `json:"namespace,omitempty"`
	EnvProcess  string            `json:"envProcess,omitempty"`
	EnvVars     map[string]string `json:"envVars,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Limits      *FunctionLimits   `json:"limits,omitempty"`
	Requests    *FunctionLimits   `json:"requests,omitempty"`
}

// FunctionLimits represents resource limits.
type FunctionLimits struct {
	Memory string `json:"memory,omitempty"`
	CPU    string `json:"cpu,omitempty"`
}

// deployFunction deploys or updates a function via the OpenFaaS gateway.
func (p *plugin) deployFunction(req DeployRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("functions: marshal deploy request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, p.config.GatewayURL+"/system/functions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("functions: create deploy request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("functions: deploy request failed: %w", err)
	}
	defer resp.Body.Close()

	// Function already exists — update it
	if resp.StatusCode == http.StatusConflict {
		httpReq, err = http.NewRequest(http.MethodPut, p.config.GatewayURL+"/system/functions", bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("functions: create update request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err = p.client.Do(httpReq)
		if err != nil {
			return fmt.Errorf("functions: update request failed: %w", err)
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("functions: deploy failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// listFunctions lists all functions from the OpenFaaS gateway.
func (p *plugin) listFunctions(namespace string) ([]OpenFaaSFunction, error) {
	url := p.config.GatewayURL + "/system/functions"
	if namespace != "" {
		url += "?namespace=" + namespace
	}

	resp, err := p.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("functions: list request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("functions: list failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var functions []OpenFaaSFunction
	if err := json.NewDecoder(resp.Body).Decode(&functions); err != nil {
		return nil, fmt.Errorf("functions: decode response: %w", err)
	}

	return functions, nil
}

// getFunction gets details about a specific function.
func (p *plugin) getFunction(name, namespace string) (*OpenFaaSFunction, error) {
	url := fmt.Sprintf("%s/system/function/%s", p.config.GatewayURL, name)
	if namespace != "" {
		url += "?namespace=" + namespace
	}

	resp, err := p.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("functions: get request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("functions: get failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var fn OpenFaaSFunction
	if err := json.NewDecoder(resp.Body).Decode(&fn); err != nil {
		return nil, fmt.Errorf("functions: decode response: %w", err)
	}

	return &fn, nil
}

// deleteFunction removes a function from OpenFaaS.
func (p *plugin) deleteFunction(name, namespace string) error {
	body, _ := json.Marshal(map[string]string{
		"functionName": name,
		"namespace":    namespace,
	})

	req, err := http.NewRequest(http.MethodDelete, p.config.GatewayURL+"/system/functions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("functions: create delete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("functions: delete request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("functions: delete failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// invokeFunction invokes a function and returns the response body, status code, and error.
func (p *plugin) invokeFunction(name string, payload []byte, async bool) ([]byte, int, error) {
	url := p.config.GatewayURL + "/function/" + name
	if async {
		url = p.config.GatewayURL + "/async-function/" + name
	}

	resp, err := p.client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, 0, fmt.Errorf("functions: invoke request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("functions: read response: %w", err)
	}

	return body, resp.StatusCode, nil
}
