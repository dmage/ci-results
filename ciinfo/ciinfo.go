package ciinfo

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Env struct {
	Name    string `json:name"`
	Default string `json:"default"`
}

type Step struct {
	As   string `json:"as"`
	From string `json:"from"`
	Env  []Env  `json:"env"`
}

type LiteralSteps struct {
	ClusterProfile string `json:"cluster_profile"`
	Pre            []Step `json:"pre"`
	Test           []Step `json:"test"`
	Post           []Step `json:"post"`
}

type Test struct {
	As           string       `json:"as"`
	Cron         string       `json:"cron"`
	LiteralSteps LiteralSteps `json:"literal_steps"`
}

type GeneratedMetadata struct {
	Org     string `json:"org"`
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Variant string `json:"variant"`
}

type Config struct {
	ZZGeneratedMetadata GeneratedMetadata `json:"zz_generated_metadata"`
	Tests               []Test            `json:"tests"`
}

func DownloadConfig(org, repo, branch, variant string) (*Config, error) {
	req, err := http.NewRequest("GET", "https://config.ci.openshift.org/config", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for configresolver: %w", err)
	}
	query := req.URL.Query()
	query.Add("org", org)
	query.Add("repo", repo)
	query.Add("branch", branch)
	if variant != "" {
		query.Add("variant", variant)
	}
	req.URL.RawQuery = query.Encode()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to configresolver: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got unexpected http response from configresolver: %s", resp.Status)
	}
	var data Config
	err = json.NewDecoder(resp.Body).Decode(&data)
	return &data, err
}

func getEnv(envs []Env, key string) string {
	for _, env := range envs {
		if env.Name == key {
			return env.Default
		}
	}
	return ""
}

func Tags(test Test) []string {
	var tags []string
	tags = append(tags, "x-platform-"+test.LiteralSteps.ClusterProfile)
	foundTest := false
	for _, step := range test.LiteralSteps.Test {
		switch step.As {
		case "openshift-e2e-test", "openshift-e2e-libvirt-test", "baremetalds-e2e-test":
			tag := "x-test-openshift-e2e-"
			switch getEnv(step.Env, "TEST_TYPE") {
			case "suite", "conformance-serial", "conformance-parallel":
				tag += "suite-"
				switch getEnv(step.Env, "TEST_SUITE") {
				case "openshift/conformance/parallel":
					tag += "parallel"
				case "openshift/conformance/serial":
					tag += "serial"
				case "openshift/csi":
					tag += "csi"
				case "experimental/reliability/minimal":
					tag += "canary"
				default:
					tag += "unknown"
				}
			case "upgrade-conformance":
				tag += "upgrade-conformance"
			case "upgrade":
				tag += "upgrade-only"
			case "image-ecosystem":
				tag += "image-ecosystem"
			case "jenkins-e2e-rhel-only":
				tag += "jenkins-e2e-rhel-only"
			default:
				tag += "unknown"
			}
			foundTest = true
			tags = append(tags, tag)
		}
	}
	if !foundTest {
		tags = append(tags, "x-test-unknown")
	}
	return tags
}

type Tagger struct {
	jobs map[string][]string
}

func NewTagger() *Tagger {
	return &Tagger{
		jobs: make(map[string][]string),
	}
}

func (t *Tagger) AddConfig(cfg *Config) {
	jobPrefix := fmt.Sprintf("periodic-ci-%s-%s-%s-", cfg.ZZGeneratedMetadata.Org, cfg.ZZGeneratedMetadata.Repo, cfg.ZZGeneratedMetadata.Branch)
	if cfg.ZZGeneratedMetadata.Variant != "" {
		jobPrefix += cfg.ZZGeneratedMetadata.Variant + "-"
	}

	for _, test := range cfg.Tests {
		jobName := jobPrefix + test.As
		t.jobs[jobName] = Tags(test)
	}
}

func (t *Tagger) GetTags(jobName string) []string {
	tags := t.jobs[jobName]
	if len(tags) == 0 {
		return []string{"x-no-steps"}
	}
	return tags
}
