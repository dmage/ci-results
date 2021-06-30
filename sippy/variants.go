package sippy

import (
	"regexp"

	"k8s.io/klog/v2"
)

var (
	// variant regexes
	awsRegex   = regexp.MustCompile(`(?i)-aws`)
	azureRegex = regexp.MustCompile(`(?i)-azure`)
	fipsRegex  = regexp.MustCompile(`(?i)-fips`)
	metalRegex = regexp.MustCompile(`(?i)-metal`)
	// metal-assisted jobs do not have a trailing -version segment
	metalAssistedRegex = regexp.MustCompile(`(?i)-metal-assisted`)
	// metal-ipi jobs do not have a trailing -version segment
	metalIPIRegex = regexp.MustCompile(`(?i)-metal-ipi`)
	// 3.11 gcp jobs don't have a trailing -version segment
	gcpRegex       = regexp.MustCompile(`(?i)-gcp`)
	openstackRegex = regexp.MustCompile(`(?i)-openstack`)
	osdRegex       = regexp.MustCompile(`(?i)-osd`)
	ovirtRegex     = regexp.MustCompile(`(?i)-ovirt`)
	ovnRegex       = regexp.MustCompile(`(?i)-ovn`)
	// proxy jobs do not have a trailing -version segment
	proxyRegex   = regexp.MustCompile(`(?i)-proxy`)
	promoteRegex = regexp.MustCompile(`(?i)^promote-`)
	ppc64leRegex = regexp.MustCompile(`(?i)-ppc64le`)
	rtRegex      = regexp.MustCompile(`(?i)-rt`)
	s390xRegex   = regexp.MustCompile(`(?i)-s390x`)
	serialRegex  = regexp.MustCompile(`(?i)-serial`)
	upgradeRegex = regexp.MustCompile(`(?i)-upgrade`)
	// some vsphere jobs do not have a trailing -version segment
	vsphereRegex    = regexp.MustCompile(`(?i)-vsphere`)
	vsphereUPIRegex = regexp.MustCompile(`(?i)-vsphere-upi`)
)

func IdentifyVariants(jobName string) []string {
	variants := []string{}

	// if it's a promotion job, it can't be a part of any other variant aggregation
	if promoteRegex.MatchString(jobName) {
		variants = append(variants, "promote")
		return variants
	}

	if awsRegex.MatchString(jobName) {
		variants = append(variants, "aws")
	}
	if azureRegex.MatchString(jobName) {
		variants = append(variants, "azure")
	}
	if gcpRegex.MatchString(jobName) {
		variants = append(variants, "gcp")
	}
	if openstackRegex.MatchString(jobName) {
		variants = append(variants, "openstack")
	}

	if osdRegex.MatchString(jobName) {
		variants = append(variants, "osd")
	}

	// Without support for negative lookbacks in the native
	// regexp library, it's easiest to differentiate these
	// three by seeing if it's metal-assisted or metal-ipi, and then fall through
	// to check if it's UPI metal.
	if metalAssistedRegex.MatchString(jobName) {
		variants = append(variants, "metal-assisted")
	} else if metalIPIRegex.MatchString(jobName) {
		variants = append(variants, "metal-ipi")
	} else if metalRegex.MatchString(jobName) {
		variants = append(variants, "metal-upi")
	}

	if ovirtRegex.MatchString(jobName) {
		variants = append(variants, "ovirt")
	}
	if vsphereUPIRegex.MatchString(jobName) {
		variants = append(variants, "vsphere-upi")
	} else if vsphereRegex.MatchString(jobName) {
		variants = append(variants, "vsphere-ipi")
	}

	if upgradeRegex.MatchString(jobName) {
		variants = append(variants, "upgrade")
	}
	if serialRegex.MatchString(jobName) {
		variants = append(variants, "serial")
	}
	if ovnRegex.MatchString(jobName) {
		variants = append(variants, "ovn")
	}
	if fipsRegex.MatchString(jobName) {
		variants = append(variants, "fips")
	}
	if ppc64leRegex.MatchString(jobName) {
		variants = append(variants, "ppc64le")
	}
	if s390xRegex.MatchString(jobName) {
		variants = append(variants, "s390x")
	}
	if rtRegex.MatchString(jobName) {
		variants = append(variants, "realtime")
	}
	if proxyRegex.MatchString(jobName) {
		variants = append(variants, "proxy")
	}

	if len(variants) == 0 {
		klog.V(2).Infof("unknown variant for job: %s\n", jobName)
		return []string{"unknown-variant"}
	}

	return variants
}
