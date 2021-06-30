package testgrid

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"k8s.io/klog/v2"
)

type TestStatus int

// https://github.com/GoogleCloudPlatform/testgrid/blob/b52feda0e27a01ddc9eca16fe17e6b29c8193b7c/pb/test_status/test_status.proto
const (
	TestStatusNoResult      TestStatus = 0
	TestStatusPass          TestStatus = 1
	TestStatusPassWithSkips TestStatus = 3
	TestStatusRunning       TestStatus = 4
	TestStatusFail          TestStatus = 12
	TestStatusFlaky         TestStatus = 13
)

type TestResult struct {
	Count int        `json:"count"`
	Value TestStatus `json:"value"`
}

type Test struct {
	Name         string       `json:"name"`
	OriginalName string       `json:"original-name"`
	Messages     []string     `json:"messages"`
	ShortTexts   []string     `json:"short-texts"`
	Statuses     []TestResult `json:"statuses"`
	Target       string       `json:"target"`
}

type JobResults struct {
	Query       string   `json:"query"`
	Changelists []string `json:"changelists"`
	Tests       []Test   `json:"tests"`
	Timestamps  []int64  `json:"timestamps"`
}

type JobSummary struct {
}

type DashboardSummary map[string]JobSummary

func dashboardSummaryURL(dashboard string) *url.URL {
	return &url.URL{
		Scheme: "https",
		Host:   "testgrid.k8s.io",
		Path:   fmt.Sprintf("/%s/summary", url.PathEscape(dashboard)),
	}
}

func jobResultsURL(dashboard, jobName string) *url.URL {
	return &url.URL{
		Scheme: "https",
		Host:   "testgrid.k8s.io",
		Path:   fmt.Sprintf("/%s/table", url.PathEscape(dashboard)),
		RawQuery: url.Values{
			"tab":              {jobName},
			"show-stale-tests": {""},
		}.Encode(),
	}
}

func GetDashboardSummary(dashboard string) (DashboardSummary, error) {
	u := dashboardSummaryURL(dashboard).String()
	klog.V(2).Infof("downloading summary for %s from %s...", dashboard, u)
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var summary DashboardSummary
	err = json.NewDecoder(resp.Body).Decode(&summary)
	return summary, err
}

func GetJobResults(dashboard, jobName string) (*JobResults, error) {
	u := jobResultsURL(dashboard, jobName).String()
	klog.V(2).Infof("downloading job results from %s...", u)
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var results JobResults
	err = json.NewDecoder(resp.Body).Decode(&results)
	return &results, err
}
