package indexer

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/dmage/ci-results/database"
	"github.com/dmage/ci-results/sippy"
	"github.com/dmage/ci-results/testgrid"
	"github.com/paulbellamy/ratecounter"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

type workers struct {
	groups sync.WaitGroup
	mu     sync.Mutex
	err    error
}

func (w *workers) saveErr(err error) {
	if err == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err == nil {
		w.err = err
	}
}

func (w *workers) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (w *workers) spawn(n int, fn func() error, finalize func() error) {
	w.groups.Add(1)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			w.saveErr(fn())
		}()
	}
	go func() {
		defer w.groups.Done()
		wg.Wait()
		w.saveErr(finalize())
	}()
}

func (w *workers) Done() error {
	w.groups.Wait()
	return w.Err()
}

type job struct {
	Dashboard string
	Name      string
}

type build struct {
	JobName   string
	Number    string
	Timestamp int64
	Tests     map[string]testgrid.TestStatus
}

type jobResults struct {
	Changelists []string
	Timestamps  []int64
	Tests       map[string][]testgrid.TestStatus
}

func unpackTestStatuses(tr []testgrid.TestResult) []testgrid.TestStatus {
	var result []testgrid.TestStatus
	for _, r := range tr {
		for i := 0; i < r.Count; i++ {
			result = append(result, r.Value)
		}
	}
	return result
}

func unpackJobResults(packedResults *testgrid.JobResults) jobResults {
	results := jobResults{
		Changelists: packedResults.Changelists,
		Timestamps:  packedResults.Timestamps,
		Tests:       make(map[string][]testgrid.TestStatus),
	}
	for _, test := range packedResults.Tests {
		results.Tests[test.Name] = unpackTestStatuses(test.Statuses)
	}
	return results
}

type regexpTagger struct {
	Tag     string
	Pattern *regexp.Regexp
}

func newRegexpTagger(tag, pattern string) regexpTagger {
	return regexpTagger{
		Tag:     tag,
		Pattern: regexp.MustCompile(pattern),
	}
}

func joinPatterns(taggers []regexpTagger) string {
	if len(taggers) == 0 {
		return ""
	}
	r := "(?:" + taggers[0].Pattern.String()
	for _, t := range taggers[1:] {
		r += "|" + t.Pattern.String()
	}
	r += ")"
	return r
}

var platforms = []regexpTagger{
	newRegexpTagger("aws-upi", "-aws-upi"),
	newRegexpTagger("azure", "-azure"),
	newRegexpTagger("gcp", "-gcp"),
	newRegexpTagger("metal-assisted", "-metal-assisted"),
	newRegexpTagger("metal-ipi", "-metal-ipi"),
	newRegexpTagger("openstack", "-openstack"),
	newRegexpTagger("ovirt", "-ovirt"),
	newRegexpTagger("libvirt-ppc64le", "-libvirt-ppc64le"),
	newRegexpTagger("libvirt-s390x", "-libvirt-s390x"),
	newRegexpTagger("vsphere-upi", "-vsphere-upi"),

	// more generic platforms should go after more specific ones
	newRegexpTagger("aws", "-aws"),
	newRegexpTagger("metal", "-metal"),
	newRegexpTagger("vsphere", "-vsphere"),
}

var mods = []regexpTagger{
	newRegexpTagger("calico", "-calico"),
	newRegexpTagger("canary", "-canary"),
	newRegexpTagger("cilium", "-cilium"),
	newRegexpTagger("compact", "-compact"),
	newRegexpTagger("disruptive", "-disruptive"),
	newRegexpTagger("fips", "-fips"),
	newRegexpTagger("mirrors", "-mirrors"),
	newRegexpTagger("ovn", "-ovn"),
	newRegexpTagger("proxy", "-proxy"),
	newRegexpTagger("rt", "-rt"),
	newRegexpTagger("sdn-multitenant", "-sdn-multitenant"),
	newRegexpTagger("shared-vpc", "-shared-vpc"),
	newRegexpTagger("single-node", "-single-node"),
}

var testTypes = []regexpTagger{
	newRegexpTagger("promote", "^promote-"),

	newRegexpTagger("conformance-serial", "-serial"),

	newRegexpTagger("other", "-arcconformance"),
	newRegexpTagger("other", "-cert-rotation"),
	newRegexpTagger("other", "-cluster-logging-operator"),
	newRegexpTagger("other", "-console"),
	newRegexpTagger("other", "-csi"),
	newRegexpTagger("other", "-elasticsearch-operator"),
	newRegexpTagger("other", "-image-ecosystem"),
	newRegexpTagger("other", "-jenkins-e2e"),

	newRegexpTagger("upgrade-conformance-from-stable", "-upgrade-from-stable"),
	newRegexpTagger("upgrade-conformance", "-upgrade"),

	newRegexpTagger("conformance-parallel", joinPatterns(platforms)+joinPatterns(mods)+"?(?:-4.[0-9]+)?$"),
}

func getTag(jobName string, taggers []regexpTagger, fallback string) string {
	for _, t := range taggers {
		if t.Pattern.MatchString(jobName) {
			return t.Tag
		}
	}
	return fallback
}

func jobTags(jobName string) database.JobTags {
	return database.JobTags{
		Platform: getTag(jobName, platforms, "unknown"),
		Mod:      getTag(jobName, mods, "none"),
		TestType: getTag(jobName, testTypes, "other"),
		Sippy:    sippy.IdentifyVariants(jobName),
	}
}

type IndexerOptions struct {
}

func (opts *IndexerOptions) Run(ctx context.Context) (err error) {
	db, err := database.OpenDefault()
	if err != nil {
		return fmt.Errorf("unable to open database: %w", err)
	}
	defer func() {
		closeErr := db.Close()
		if err == nil {
			err = closeErr
		}
	}()

	var w workers
	jobsCh := make(chan job, 100)
	buildsCh := make(chan build, 1000)

	w.spawn(1, func() error {
		for _, dashboard := range []string{
			"redhat-openshift-ocp-release-4.8-blocking",
			"redhat-openshift-ocp-release-4.8-informing",
		} {
			summary, err := testgrid.GetDashboardSummary(dashboard)
			if err != nil {
				return err
			}

			for jobName := range summary {
				jobsCh <- job{
					Dashboard: dashboard,
					Name:      jobName,
				}
			}
		}
		return nil
	}, func() error {
		close(jobsCh)
		return nil
	})

	w.spawn(5, func() error {
		for job := range jobsCh {
			packedResults, err := testgrid.GetJobResults(job.Dashboard, job.Name)
			if err != nil {
				return err
			}
			results := unpackJobResults(packedResults)
			for i, id := range results.Changelists {
				build := build{
					JobName:   job.Name,
					Number:    id,
					Timestamp: results.Timestamps[i],
					Tests:     make(map[string]testgrid.TestStatus),
				}
				for testName, statuses := range results.Tests {
					status := statuses[i]
					if status == testgrid.TestStatusNoResult {
						continue
					}
					build.Tests[testName] = status
				}
				buildsCh <- build
			}
		}
		return nil
	}, func() error {
		close(buildsCh)
		return nil
	})

	counter := ratecounter.NewRateCounter(1 * time.Second)
	go func() {
		for {
			klog.Infof("INSERT RATE: %v", counter.Rate())
			time.Sleep(1 * time.Second)
		}
	}()
	w.spawn(1, func() (err error) {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer func() {
			commitErr := tx.Commit()
			if err == nil {
				err = commitErr
			}
		}()

		for build := range buildsCh {
			running := false
			for _, status := range build.Tests {
				if status == testgrid.TestStatusRunning {
					running = true
					break
				}
			}
			if running {
				continue
			}

			buildStatus := 1 // Success
			if build.Tests["Overall"] == testgrid.TestStatusFail {
				buildStatus = 2
			}

			jobID, err := tx.FindJob(build.JobName)
			if database.IsNotFound(err) {
				jobID, err = tx.InsertJob(build.JobName, jobTags(build.JobName))
				if err != nil {
					return err
				}
			} else if err != nil {
				return err
			}

			buildID, err := tx.UpsertBuild(jobID, build.Number, build.Timestamp, buildStatus)
			if err != nil {
				return err
			}

			for testName, status := range build.Tests {
				testID, err := tx.UpsertTest(testName)
				if err != nil {
					return err
				}

				err = tx.UpsertTestResult(buildID, testID, status)
				if err != nil {
					return err
				}
				counter.Incr(1)
			}
		}
		return nil
	}, func() error {
		return nil
	})

	return w.Done()
}

func NewCmdIndexer() *cobra.Command {
	opts := &IndexerOptions{}

	cmd := &cobra.Command{
		Use:   "indexer",
		Short: "Gather data from TestGrid",
		Long: heredoc.Doc(`
			Collect test results from TestGrid and store them into the database.
		`),
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			err := opts.Run(cmd.Context())
			if err != nil {
				klog.Exit(err)
			}
		},
	}

	return cmd
}
