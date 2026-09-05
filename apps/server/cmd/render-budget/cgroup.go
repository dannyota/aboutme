package main

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const cgroupRoot = "/sys/fs/cgroup"

func readCgroupEvidence() (cgroupEvidence, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return cgroupEvidence{}, errors.New("cgroup_unavailable")
	}
	path := ""
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "0::/") || line == "0::/" {
			path = strings.TrimPrefix(line, "0::")
			break
		}
	}
	if path == "" || !strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
		return cgroupEvidence{}, errors.New("cgroup_v2_required")
	}
	directory := filepath.Join(cgroupRoot, filepath.FromSlash(strings.TrimPrefix(path, "/")))
	memoryMax, err := readTrimmed(filepath.Join(directory, "memory.max"))
	if err != nil {
		return cgroupEvidence{}, errors.New("cgroup_v2_required")
	}
	swapMax, err := readTrimmed(filepath.Join(directory, "memory.swap.max"))
	if err != nil {
		return cgroupEvidence{}, errors.New("cgroup_v2_required")
	}
	cpuMax, err := readTrimmed(filepath.Join(directory, "cpu.max"))
	if err != nil {
		return cgroupEvidence{}, errors.New("cgroup_v2_required")
	}
	events, err := readMemoryEvents(filepath.Join(directory, "memory.events"))
	if err != nil {
		return cgroupEvidence{}, errors.New("cgroup_v2_required")
	}
	evidence := cgroupEvidence{Version: 2, Path: path, MemoryMax: memoryMax, MemorySwapMax: swapMax, CPUMax: cpuMax, MemoryEventsBefore: events}
	if memoryMax != "536870912" || swapMax != "0" || !halfCPUQuota(cpuMax) {
		return evidence, errors.New("cgroup_limits_invalid")
	}
	return evidence, nil
}

func finishCgroupEvidence(evidence *cgroupEvidence) error {
	directory := filepath.Join(cgroupRoot, filepath.FromSlash(strings.TrimPrefix(evidence.Path, "/")))
	peak, err := readTrimmed(filepath.Join(directory, "memory.peak"))
	if err != nil {
		return errors.New("cgroup_read_failed")
	}
	events, err := readMemoryEvents(filepath.Join(directory, "memory.events"))
	if err != nil {
		return errors.New("cgroup_read_failed")
	}
	evidence.MemoryPeak = peak
	evidence.MemoryEventsAfter = events
	return nil
}

func readTrimmed(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func readMemoryEvents(path string) (map[string]uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := map[string]uint64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			return nil, errors.New("invalid memory events")
		}
		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			return nil, parseErr
		}
		result[fields[0]] = value
	}
	if err := scanner.Err(); err != nil || len(result) == 0 {
		return nil, errors.New("invalid memory events")
	}
	return result, nil
}

func halfCPUQuota(value string) bool {
	fields := strings.Fields(value)
	if len(fields) != 2 || fields[0] == "max" {
		return false
	}
	quota, err1 := strconv.ParseUint(fields[0], 10, 64)
	period, err2 := strconv.ParseUint(fields[1], 10, 64)
	return err1 == nil && err2 == nil && period > 0 && quota <= ^uint64(0)/2 && quota*2 == period
}
