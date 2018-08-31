package procdiscovery

import (
	"encoding/json"
	"fmt"

	"github.com/DataDog/datadog-agent/pkg/procmatch"
	"github.com/DataDog/datadog-agent/pkg/status"
	"github.com/shirou/gopsutil/process"
)

// IntegrationProcess represents a process that matches an integration
type IntegrationProcess struct {
	Cmd         string `json:"cmd"`          // The command line that matched the integration
	DisplayName string `json:"display_name"` // The integration display name
	Name        string `json:"name"`         // The integration name
}

// DiscoveredIntegrations is a map whose keys are integrations names and values are lists of IntegrationProcess
type DiscoveredIntegrations struct {
	Discovered map[string][]IntegrationProcess
	Running    map[string]struct{}
	Failing    map[string]struct{}
}

// DiscoverIntegrations retrieves processes running on the host and tries to find possible integrations
func DiscoverIntegrations() (DiscoveredIntegrations, error) {
	di := DiscoveredIntegrations{}
	matcher, err := procmatch.NewDefault()

	if err != nil {
		return di, fmt.Errorf("Couldn't build default process matcher: %s", err)
	}

	processes, err := process.Processes()

	if err != nil {
		return di, fmt.Errorf("Couldn't retrieve process list: %s", err)
	}

	running, failing, err := retrieveIntegrations()
	if err != nil {
		return di, err
	}

	// processList is a set of processes (removes duplicate processes)
	processList := map[string]struct{}{}
	for _, p := range processes {
		cmd, err := p.Cmdline()
		if err != nil {
			continue
		}
		processList[cmd] = struct{}{}
	}

	integrations := map[string][]IntegrationProcess{}

	// Try to find an integration for each process
	for process := range processList {
		matched := matcher.Match(process)
		if len(matched.Name) == 0 {
			continue
		}

		if _, ok := integrations[matched.Name]; !ok {
			integrations[matched.Name] = []IntegrationProcess{}
		}

		integrations[matched.Name] = append(integrations[matched.Name], IntegrationProcess{
			Cmd:         process,
			DisplayName: matched.DisplayName,
			Name:        matched.Name,
		})
	}
	di.Discovered = integrations
	di.Running = running
	di.Failing = failing

	return di, nil
}

func retrieveIntegrations() (map[string]struct{}, map[string]struct{}, error) {
	running := map[string]struct{}{}
	failing := map[string]struct{}{}

	statusRaw, err := status.GetStatus()

	if err != nil {
		return running, failing, fmt.Errorf("Couldn't retrieve agent status: %s", err)
	}

	statusBytes, err := json.Marshal(statusRaw)
	if err != nil {
		return running, failing, fmt.Errorf("Couldn't marshal agent status: %s", err)
	}

	var status struct {
		RunnerStats struct {
			Checks map[string]interface{}
		} `json:"runnerStats"`
		CheckSchedulerStats struct {
			LoaderErrors map[string]interface{}
		} `json:"checkSchedulerStats"`
	}

	err = json.Unmarshal(statusBytes, &status)
	if err != nil {
		return running, failing, fmt.Errorf("Couldn't decode agent status response: %s", err)
	}

	for key := range status.RunnerStats.Checks {
		running[key] = struct{}{}
	}
	for key := range status.CheckSchedulerStats.LoaderErrors {
		failing[key] = struct{}{}
	}

	return running, failing, nil
}