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
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

/*
 * Execute the Slurm sdiag command to read the current statistics
 * from the Slurm scheduler. It will be repreatedly called by the
 * collector.
 */

// Basic metrics for the scheduler
type SchedulerMetrics struct {
	threads                           float64
	queue_size                        float64
	dbd_queue_size                    float64
	last_cycle                        float64
	mean_cycle                        float64
	cycle_per_minute                  float64
	backfill_last_cycle               float64
	backfill_mean_cycle               float64
	backfill_depth_mean               float64
	total_backfilled_jobs_since_start float64
	total_backfilled_jobs_since_cycle float64
	total_backfilled_heterogeneous    float64
	rpc_stats_count                   map[string]float64
	rpc_stats_avg_time                map[string]float64
	rpc_stats_total_time              map[string]float64
	user_rpc_stats_count              map[string]float64
	user_rpc_stats_avg_time           map[string]float64
	user_rpc_stats_total_time         map[string]float64
}

// Execute the sdiag command and return its output
func SchedulerData() []byte {
	cmd := exec.Command("/usr/bin/sdiag")
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

// Extract the relevant metrics from the sdiag output
func ParseSchedulerMetrics(input []byte) *SchedulerMetrics {
	var sm SchedulerMetrics
	lines := strings.Split(string(input), "\n")
	// Guard variables to check for string repetitions in the output of sdiag
	// (two occurencies of the following strings: 'Last cycle', 'Mean cycle')
	lc_count := 0
	mc_count := 0
	for _, line := range lines {
		if strings.Contains(line, ":") {
			state := strings.Split(line, ":")[0]
			st := regexp.MustCompile(`^Server thread`)
			qs := regexp.MustCompile(`^Agent queue`)
			dbd := regexp.MustCompile(`^DBD Agent`)
			lc := regexp.MustCompile(`^[\s]+Last cycle$`)
			mc := regexp.MustCompile(`^[\s]+Mean cycle$`)
			cpm := regexp.MustCompile(`^[\s]+Cycles per`)
			dpm := regexp.MustCompile(`^[\s]+Depth Mean$`)
			tbs := regexp.MustCompile(`^[\s]+Total backfilled jobs \(since last slurm start\)`)
			tbc := regexp.MustCompile(`^[\s]+Total backfilled jobs \(since last stats cycle start\)`)
			tbh := regexp.MustCompile(`^[\s]+Total backfilled heterogeneous job components`)
			switch {
			case st.MatchString(state):
				sm.threads, _ = strconv.ParseFloat(strings.TrimSpace(strings.Split(line, ":")[1]), 64)
			case qs.MatchString(state):
				sm.queue_size, _ = strconv.ParseFloat(strings.TrimSpace(strings.Split(line, ":")[1]), 64)
			case dbd.MatchString(state):
				sm.dbd_queue_size, _ = strconv.ParseFloat(strings.TrimSpace(strings.Split(line, ":")[1]), 64)
			case lc.MatchString(state):
				if lc_count == 0 {
					sm.last_cycle, _ = strconv.ParseFloat(strings.TrimSpace(strings.Split(line, ":")[1]), 64)
					lc_count = 1
				}
				if lc_count == 1 {
					sm.backfill_last_cycle, _ = strconv.ParseFloat(strings.TrimSpace(strings.Split(line, ":")[1]), 64)
				}
			case mc.MatchString(state):
				if mc_count == 0 {
					sm.mean_cycle, _ = strconv.ParseFloat(strings.TrimSpace(strings.Split(line, ":")[1]), 64)
					mc_count = 1
				}
				if mc_count == 1 {
					sm.backfill_mean_cycle, _ = strconv.ParseFloat(strings.TrimSpace(strings.Split(line, ":")[1]), 64)
				}
			case cpm.MatchString(state):
				sm.cycle_per_minute, _ = strconv.ParseFloat(strings.TrimSpace(strings.Split(line, ":")[1]), 64)
			case dpm.MatchString(state):
				sm.backfill_depth_mean, _ = strconv.ParseFloat(strings.TrimSpace(strings.Split(line, ":")[1]), 64)
			case tbs.MatchString(state):
				sm.total_backfilled_jobs_since_start, _ = strconv.ParseFloat(strings.TrimSpace(strings.Split(line, ":")[1]), 64)
			case tbc.MatchString(state):
				sm.total_backfilled_jobs_since_cycle, _ = strconv.ParseFloat(strings.TrimSpace(strings.Split(line, ":")[1]), 64)
			case tbh.MatchString(state):
				sm.total_backfilled_heterogeneous, _ = strconv.ParseFloat(strings.TrimSpace(strings.Split(line, ":")[1]), 64)
			}
		}
	}
	rpc_stats := ParseRpcStats(lines)
	sm.rpc_stats_count = rpc_stats[0]
	sm.rpc_stats_avg_time = rpc_stats[1]
	sm.rpc_stats_total_time = rpc_stats[2]
	sm.user_rpc_stats_count = rpc_stats[3]
	sm.user_rpc_stats_avg_time = rpc_stats[4]
	sm.user_rpc_stats_total_time = rpc_stats[5]
	return &sm
}

// Helper function to split a single line from the sdiag output
func SplitColonValueToFloat(input string) float64 {
	str := strings.Split(input, ":")
	if len(str) == 1 {
		return 0
	} else {
		rvalue := strings.TrimSpace(str[1])
		flt, _ := strconv.ParseFloat(rvalue, 64)
		return flt
	}
}

// Helper function to return RPC stats from sdiag output
func ParseRpcStats(lines []string) []map[string]float64 {
	var in_rpc bool
	var in_rpc_per_user bool
	var count_stats map[string]float64
	var avg_stats map[string]float64
	var total_stats map[string]float64
	var user_count_stats map[string]float64
	var user_avg_stats map[string]float64
	var user_total_stats map[string]float64

	count_stats = make(map[string]float64)
	avg_stats = make(map[string]float64)
	total_stats = make(map[string]float64)
	user_count_stats = make(map[string]float64)
	user_avg_stats = make(map[string]float64)
	user_total_stats = make(map[string]float64)

	in_rpc = false
	in_rpc_per_user = false

	stat_line_re := regexp.MustCompile(`^\s*([A-Za-z0-9_]*).*count:([0-9]*)\s*ave_time:([0-9]*)\s\s*total_time:([0-9]*)\s*$`)

	for _, line := range lines {
		if strings.Contains(line, "Remote Procedure Call statistics by message type") {
			in_rpc = true
			in_rpc_per_user = false
		} else if strings.Contains(line, "Remote Procedure Call statistics by user") {
			in_rpc = false
			in_rpc_per_user = true
		}
		if in_rpc || in_rpc_per_user {
			re_match := stat_line_re.FindAllStringSubmatch(line, -1)
			if re_match != nil {
				re_match_first := re_match[0]
				if in_rpc {
					count_stats[re_match_first[1]], _ = strconv.ParseFloat(re_match_first[2], 64)
					avg_stats[re_match_first[1]], _ = strconv.ParseFloat(re_match_first[3], 64)
					total_stats[re_match_first[1]], _ = strconv.ParseFloat(re_match_first[4], 64)
				} else if in_rpc_per_user {
					user_count_stats[re_match_first[1]], _ = strconv.ParseFloat(re_match_first[2], 64)
					user_avg_stats[re_match_first[1]], _ = strconv.ParseFloat(re_match_first[3], 64)
					user_total_stats[re_match_first[1]], _ = strconv.ParseFloat(re_match_first[4], 64)
				}
			}
		}
	}

	rpc_stats_final := []map[string]float64{
		count_stats,
		avg_stats,
		total_stats,
		user_count_stats,
		user_avg_stats,
		user_total_stats,
	}
	return rpc_stats_final
}

// Returns the scheduler metrics
func SchedulerGetMetrics() *SchedulerMetrics {
	return ParseSchedulerMetrics(SchedulerData())
}

/*
 * Implement the Prometheus Collector interface and feed the
 * Slurm scheduler metrics into it.
 * https://godoc.org/github.com/prometheus/client_golang/prometheus#Collector
 */

// Collector strcture
type SchedulerCollector struct {
	threads                           *prometheus.Desc
	queue_size                        *prometheus.Desc
	dbd_queue_size                    *prometheus.Desc
	last_cycle                        *prometheus.Desc
	mean_cycle                        *prometheus.Desc
	cycle_per_minute                  *prometheus.Desc
	backfill_last_cycle               *prometheus.Desc
	backfill_mean_cycle               *prometheus.Desc
	backfill_depth_mean               *prometheus.Desc
	total_backfilled_jobs_since_start *prometheus.Desc
	total_backfilled_jobs_since_cycle *prometheus.Desc
	total_backfilled_heterogeneous    *prometheus.Desc
	rpc_stats_count                   *prometheus.Desc
	rpc_stats_avg_time                *prometheus.Desc
	rpc_stats_total_time              *prometheus.Desc
	user_rpc_stats_count              *prometheus.Desc
	user_rpc_stats_avg_time           *prometheus.Desc
	user_rpc_stats_total_time         *prometheus.Desc
}

// Send all metric descriptions
func (c *SchedulerCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.threads
	ch <- c.queue_size
	ch <- c.dbd_queue_size
	ch <- c.last_cycle
	ch <- c.mean_cycle
	ch <- c.cycle_per_minute
	ch <- c.backfill_last_cycle
	ch <- c.backfill_mean_cycle
	ch <- c.backfill_depth_mean
	ch <- c.total_backfilled_jobs_since_start
	ch <- c.total_backfilled_jobs_since_cycle
	ch <- c.total_backfilled_heterogeneous
	ch <- c.rpc_stats_count
	ch <- c.rpc_stats_avg_time
	ch <- c.rpc_stats_total_time
	ch <- c.user_rpc_stats_count
	ch <- c.user_rpc_stats_avg_time
	ch <- c.user_rpc_stats_total_time
}

// Send the values of all metrics
func (sc *SchedulerCollector) Collect(ch chan<- prometheus.Metric) {
	sm := SchedulerGetMetrics()
	ch <- prometheus.MustNewConstMetric(sc.threads, prometheus.GaugeValue, sm.threads)
	ch <- prometheus.MustNewConstMetric(sc.queue_size, prometheus.GaugeValue, sm.queue_size)
	ch <- prometheus.MustNewConstMetric(sc.dbd_queue_size, prometheus.GaugeValue, sm.dbd_queue_size)
	ch <- prometheus.MustNewConstMetric(sc.last_cycle, prometheus.GaugeValue, sm.last_cycle)
	ch <- prometheus.MustNewConstMetric(sc.mean_cycle, prometheus.GaugeValue, sm.mean_cycle)
	ch <- prometheus.MustNewConstMetric(sc.cycle_per_minute, prometheus.GaugeValue, sm.cycle_per_minute)
	ch <- prometheus.MustNewConstMetric(sc.backfill_last_cycle, prometheus.GaugeValue, sm.backfill_last_cycle)
	ch <- prometheus.MustNewConstMetric(sc.backfill_mean_cycle, prometheus.GaugeValue, sm.backfill_mean_cycle)
	ch <- prometheus.MustNewConstMetric(sc.backfill_depth_mean, prometheus.GaugeValue, sm.backfill_depth_mean)
	ch <- prometheus.MustNewConstMetric(sc.total_backfilled_jobs_since_start, prometheus.GaugeValue, sm.total_backfilled_jobs_since_start)
	ch <- prometheus.MustNewConstMetric(sc.total_backfilled_jobs_since_cycle, prometheus.GaugeValue, sm.total_backfilled_jobs_since_cycle)
	ch <- prometheus.MustNewConstMetric(sc.total_backfilled_heterogeneous, prometheus.GaugeValue, sm.total_backfilled_heterogeneous)
	for rpc_type, value := range sm.rpc_stats_count {
		ch <- prometheus.MustNewConstMetric(sc.rpc_stats_count, prometheus.GaugeValue, value, rpc_type)
	}
	for rpc_type, value := range sm.rpc_stats_avg_time {
		ch <- prometheus.MustNewConstMetric(sc.rpc_stats_avg_time, prometheus.GaugeValue, value, rpc_type)
	}
	for rpc_type, value := range sm.rpc_stats_total_time {
		ch <- prometheus.MustNewConstMetric(sc.rpc_stats_total_time, prometheus.GaugeValue, value, rpc_type)
	}
	for user, value := range sm.user_rpc_stats_count {
		ch <- prometheus.MustNewConstMetric(sc.user_rpc_stats_count, prometheus.GaugeValue, value, user)
	}
	for user, value := range sm.user_rpc_stats_avg_time {
		ch <- prometheus.MustNewConstMetric(sc.user_rpc_stats_avg_time, prometheus.GaugeValue, value, user)
	}
	for user, value := range sm.user_rpc_stats_total_time {
		ch <- prometheus.MustNewConstMetric(sc.user_rpc_stats_total_time, prometheus.GaugeValue, value, user)
	}

}

// Returns the Slurm scheduler collector, used to register with the prometheus client
func NewSchedulerCollector() *SchedulerCollector {
	rpc_stats_labels := make([]string, 0, 1)
	rpc_stats_labels = append(rpc_stats_labels, "operation")
	user_rpc_stats_labels := make([]string, 0, 1)
	user_rpc_stats_labels = append(user_rpc_stats_labels, "user")
	return &SchedulerCollector{
		threads: prometheus.NewDesc(
			"slurm_scheduler_threads",
			"Information provided by the Slurm sdiag command, number of scheduler threads ",
			nil,
			nil),
		queue_size: prometheus.NewDesc(
			"slurm_scheduler_queue_size",
			"Information provided by the Slurm sdiag command, length of the scheduler queue",
			nil,
			nil),
		dbd_queue_size: prometheus.NewDesc(
			"slurm_scheduler_dbd_queue_size",
			"Information provided by the Slurm sdiag command, length of the DBD agent queue",
			nil,
			nil),
		last_cycle: prometheus.NewDesc(
			"slurm_scheduler_last_cycle",
			"Information provided by the Slurm sdiag command, scheduler last cycle time in (microseconds)",
			nil,
			nil),
		mean_cycle: prometheus.NewDesc(
			"slurm_scheduler_mean_cycle",
			"Information provided by the Slurm sdiag command, scheduler mean cycle time in (microseconds)",
			nil,
			nil),
		cycle_per_minute: prometheus.NewDesc(
			"slurm_scheduler_cycle_per_minute",
			"Information provided by the Slurm sdiag command, number scheduler cycles per minute",
			nil,
			nil),
		backfill_last_cycle: prometheus.NewDesc(
			"slurm_scheduler_backfill_last_cycle",
			"Information provided by the Slurm sdiag command, scheduler backfill last cycle time in (microseconds)",
			nil,
			nil),
		backfill_mean_cycle: prometheus.NewDesc(
			"slurm_scheduler_backfill_mean_cycle",
			"Information provided by the Slurm sdiag command, scheduler backfill mean cycle time in (microseconds)",
			nil,
			nil),
		backfill_depth_mean: prometheus.NewDesc(
			"slurm_scheduler_backfill_depth_mean",
			"Information provided by the Slurm sdiag command, scheduler backfill mean depth",
			nil,
			nil),
		total_backfilled_jobs_since_start: prometheus.NewDesc(
			"slurm_scheduler_backfilled_jobs_since_start_total",
			"Information provided by the Slurm sdiag command, number of jobs started thanks to backfilling since last slurm start",
			nil,
			nil),
		total_backfilled_jobs_since_cycle: prometheus.NewDesc(
			"slurm_scheduler_backfilled_jobs_since_cycle_total",
			"Information provided by the Slurm sdiag command, number of jobs started thanks to backfilling since last time stats where reset",
			nil,
			nil),
		total_backfilled_heterogeneous: prometheus.NewDesc(
			"slurm_scheduler_backfilled_heterogeneous_total",
			"Information provided by the Slurm sdiag command, number of heterogeneous job components started thanks to backfilling since last Slurm start",
			nil,
			nil),
		rpc_stats_count: prometheus.NewDesc(
			"slurm_rpc_stats",
			"Information provided by the Slurm sdiag command, rpc count statistic",
			rpc_stats_labels,
			nil),
		rpc_stats_avg_time: prometheus.NewDesc(
			"slurm_rpc_stats_avg_time",
			"Information provided by the Slurm sdiag command, rpc average time statistic",
			rpc_stats_labels,
			nil),
		rpc_stats_total_time: prometheus.NewDesc(
			"slurm_rpc_stats_total_time",
			"Information provided by the Slurm sdiag command, rpc total time statistic",
			rpc_stats_labels,
			nil),
		user_rpc_stats_count: prometheus.NewDesc(
			"slurm_user_rpc_stats",
			"Information provided by the Slurm sdiag command, rpc count statistic per user",
			user_rpc_stats_labels,
			nil),
		user_rpc_stats_avg_time: prometheus.NewDesc(
			"slurm_user_rpc_stats_avg_time",
			"Information provided by the Slurm sdiag command, rpc average time statistic per user",
			user_rpc_stats_labels,
			nil),
		user_rpc_stats_total_time: prometheus.NewDesc(
			"slurm_user_rpc_stats_total_time",
			"Information provided by the Slurm sdiag command, rpc total time statistic per user",
			user_rpc_stats_labels,
			nil),
	}
}
