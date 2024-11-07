/* Copyright 2017 Victor Penso, Matteo Dessalvi

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>. */

package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

type NodesMetrics struct {
	alloc   map[string]float64
	comp    map[string]float64
	down    map[string]float64
	drain   map[string]float64
	err     map[string]float64
	fail    map[string]float64
	idle    map[string]float64
	maint   map[string]float64
	mix     map[string]float64
	resv    map[string]float64
	other   map[string]float64
	planned map[string]float64
	total   map[string]float64
}

func NodesGetMetrics(part string) *NodesMetrics {
	return ParseNodesMetrics(NodesData(part))
}

func RemoveDuplicates(s []string) []string {
	m := map[string]bool{}
	t := []string{}

	// Walk through the slice 's' and for each value we haven't seen so far, append it to 't'.
	for _, v := range s {
		if _, seen := m[v]; !seen {
			if len(v) > 0 {
				t = append(t, v)
				m[v] = true
			}
		}
	}

	return t
}

func InitFeatureSet(nm *NodesMetrics, feature_set string) {
	//lint:file-ignore SA4018 If the feature set exists keep, else assign nil
	nm.alloc[feature_set] = nm.alloc[feature_set]
	nm.comp[feature_set] = nm.comp[feature_set]
	nm.down[feature_set] = nm.down[feature_set]
	nm.drain[feature_set] = nm.drain[feature_set]
	nm.err[feature_set] = nm.err[feature_set]
	nm.fail[feature_set] = nm.fail[feature_set]
	nm.idle[feature_set] = nm.idle[feature_set]
	nm.maint[feature_set] = nm.maint[feature_set]
	nm.mix[feature_set] = nm.mix[feature_set]
	nm.resv[feature_set] = nm.resv[feature_set]
	nm.other[feature_set] = nm.other[feature_set]
	nm.planned[feature_set] = nm.planned[feature_set]
	nm.total[feature_set] = nm.total[feature_set]
}

func ParseNodesMetrics(input []byte) *NodesMetrics {
	var nm NodesMetrics
	var feature_set string
	lines := strings.Split(string(input), "\n")

	// Sort and remove all the duplicates from the 'sinfo' output
	sort.Strings(lines)
	lines_uniq := RemoveDuplicates(lines)

	nm.alloc = make(map[string]float64)
	nm.comp = make(map[string]float64)
	nm.down = make(map[string]float64)
	nm.drain = make(map[string]float64)
	nm.err = make(map[string]float64)
	nm.fail = make(map[string]float64)
	nm.idle = make(map[string]float64)
	nm.maint = make(map[string]float64)
	nm.mix = make(map[string]float64)
	nm.resv = make(map[string]float64)
	nm.other = make(map[string]float64)
	nm.planned = make(map[string]float64)
	nm.total = make(map[string]float64)

	for _, line := range lines_uniq {
		if strings.Contains(line, "|") {
			split := strings.Split(line, "|")
			state := split[1]
			count, _ := strconv.ParseFloat(strings.TrimSpace(split[0]), 64)
			features := strings.Split(split[2], ",")
			sort.Strings(features)
			feature_set = strings.Join(features[:], ",")
			if feature_set == "(null)" {
				feature_set = "null"
			}
			InitFeatureSet(&nm, feature_set)
			alloc := regexp.MustCompile(`^alloc`)
			comp := regexp.MustCompile(`^comp`)
			down := regexp.MustCompile(`^down`)
			drain := regexp.MustCompile(`^drain`)
			fail := regexp.MustCompile(`^fail`)
			err := regexp.MustCompile(`^err`)
			idle := regexp.MustCompile(`^idle`)
			maint := regexp.MustCompile(`^maint`)
			mix := regexp.MustCompile(`^mix`)
			resv := regexp.MustCompile(`^res`)
			planned := regexp.MustCompile(`^planned`)
			switch {
			case alloc.MatchString(state):
				nm.alloc[feature_set] += count
			case comp.MatchString(state):
				nm.comp[feature_set] += count
			case down.MatchString(state):
				nm.down[feature_set] += count
			case drain.MatchString(state):
				nm.drain[feature_set] += count
			case fail.MatchString(state):
				nm.fail[feature_set] += count
			case err.MatchString(state):
				nm.err[feature_set] += count
			case idle.MatchString(state):
				nm.idle[feature_set] += count
			case maint.MatchString(state):
				nm.maint[feature_set] += count
			case mix.MatchString(state):
				nm.mix[feature_set] += count
			case resv.MatchString(state):
				nm.resv[feature_set] += count
			case planned.MatchString(state):
				nm.planned[feature_set] += count
			default:
				nm.other[feature_set] += count
			}
		}
	}
	return &nm
}

// Execute the sinfo command and return its output
func NodesData(part string) []byte {
	cmd := exec.Command("/usr/bin/sinfo", "-h", "-o \"%D|%T|%b\"", "-p", part, "| sort", "| uniq")
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin:/usr/sbin:/sbin")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	out, _ := ioutil.ReadAll(stdout)
	if err := cmd.Wait(); err != nil {
		log.Fatal(err)
	}
	return out
}

func SlurmGetTotal() float64 {
	// cmd := exec.Command("bash", "-c", "\"/usr/bin/scontrol show nodes -o | grep -c 'NodeName=[a-z]*[0-9]*'\"")
	cmd := exec.Command("/usr/bin/scontrol", "show", "nodes", "-o", "| grep", "-c", "'NodeName=[a-z]*[0-9]*'")
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin:/usr/sbin:/sbin")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatalf("cmd.Start: %v", err)
	}
	out, _ := ioutil.ReadAll(stdout)
	err_out, _ := ioutil.ReadAll(stderr)
	if err := cmd.Wait(); err != nil {
		log.Fatalf("cmd.Wait: %v %s %s", err, out, err_out)
	}
	data := strings.Split(string(out), "\n")
	total, _ := strconv.ParseFloat(data[0], 64)
	return total
}

func SlurmGetPartitions() []string {
	cmd := exec.Command("/usr/bin/sinfo", "-h", "-o %R", "| sort", "| uniq")
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin:/usr/sbin:/sbin")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	out, _ := ioutil.ReadAll(stdout)
	if err := cmd.Wait(); err != nil {
		log.Fatal(err)
	}
	partitions := strings.Split(string(out), "\n")
	return partitions
}

/*
 * Implement the Prometheus Collector interface and feed the
 * Slurm scheduler metrics into it.
 * https://godoc.org/github.com/prometheus/client_golang/prometheus#Collector
 */

func NewNodesCollector() *NodesCollector {
	labelnames := make([]string, 0, 1)
	labelnames = append(labelnames, "partition")
	labelnames = append(labelnames, "active_feature_set")
	return &NodesCollector{
		alloc:   prometheus.NewDesc("slurm_nodes_alloc", "Allocated nodes", labelnames, nil),
		comp:    prometheus.NewDesc("slurm_nodes_comp", "Completing nodes", labelnames, nil),
		down:    prometheus.NewDesc("slurm_nodes_down", "Down nodes", labelnames, nil),
		drain:   prometheus.NewDesc("slurm_nodes_drain", "Drain nodes", labelnames, nil),
		err:     prometheus.NewDesc("slurm_nodes_err", "Error nodes", labelnames, nil),
		fail:    prometheus.NewDesc("slurm_nodes_fail", "Fail nodes", labelnames, nil),
		idle:    prometheus.NewDesc("slurm_nodes_idle", "Idle nodes", labelnames, nil),
		maint:   prometheus.NewDesc("slurm_nodes_maint", "Maint nodes", labelnames, nil),
		mix:     prometheus.NewDesc("slurm_nodes_mix", "Mix nodes", labelnames, nil),
		resv:    prometheus.NewDesc("slurm_nodes_resv", "Reserved nodes", labelnames, nil),
		other:   prometheus.NewDesc("slurm_nodes_other", "Nodes reported with an unknown state", labelnames, nil),
		planned: prometheus.NewDesc("slurm_nodes_planned", "Planned nodes", labelnames, nil),
		total:   prometheus.NewDesc("slurm_nodes_total", "Total number of nodes", nil, nil),
	}
}

type NodesCollector struct {
	alloc   *prometheus.Desc
	comp    *prometheus.Desc
	down    *prometheus.Desc
	drain   *prometheus.Desc
	err     *prometheus.Desc
	fail    *prometheus.Desc
	idle    *prometheus.Desc
	maint   *prometheus.Desc
	mix     *prometheus.Desc
	resv    *prometheus.Desc
	other   *prometheus.Desc
	planned *prometheus.Desc
	total   *prometheus.Desc
}

// Send all metric descriptions
func (nc *NodesCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- nc.alloc
	ch <- nc.comp
	ch <- nc.down
	ch <- nc.drain
	ch <- nc.err
	ch <- nc.fail
	ch <- nc.idle
	ch <- nc.maint
	ch <- nc.mix
	ch <- nc.resv
	ch <- nc.other
	ch <- nc.planned
	ch <- nc.total
}

func SendFeatureSetMetric(ch chan<- prometheus.Metric, desc *prometheus.Desc, valueType prometheus.ValueType, featurestate map[string]float64, part string) {
	for set, value := range featurestate {
		ch <- prometheus.MustNewConstMetric(desc, valueType, value, part, set)
	}
}

func (nc *NodesCollector) Collect(ch chan<- prometheus.Metric) {
	partitions := SlurmGetPartitions()
	for _, part := range partitions {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		nm := NodesGetMetrics(part)
		SendFeatureSetMetric(ch, nc.alloc, prometheus.GaugeValue, nm.alloc, part)
		SendFeatureSetMetric(ch, nc.comp, prometheus.GaugeValue, nm.comp, part)
		SendFeatureSetMetric(ch, nc.down, prometheus.GaugeValue, nm.down, part)
		SendFeatureSetMetric(ch, nc.drain, prometheus.GaugeValue, nm.drain, part)
		SendFeatureSetMetric(ch, nc.err, prometheus.GaugeValue, nm.err, part)
		SendFeatureSetMetric(ch, nc.fail, prometheus.GaugeValue, nm.fail, part)
		SendFeatureSetMetric(ch, nc.idle, prometheus.GaugeValue, nm.idle, part)
		SendFeatureSetMetric(ch, nc.maint, prometheus.GaugeValue, nm.maint, part)
		SendFeatureSetMetric(ch, nc.mix, prometheus.GaugeValue, nm.mix, part)
		SendFeatureSetMetric(ch, nc.resv, prometheus.GaugeValue, nm.resv, part)
		SendFeatureSetMetric(ch, nc.other, prometheus.GaugeValue, nm.other, part)
		SendFeatureSetMetric(ch, nc.planned, prometheus.GaugeValue, nm.planned, part)
	}
	total := SlurmGetTotal()
	ch <- prometheus.MustNewConstMetric(nc.total, prometheus.GaugeValue, total)
}
