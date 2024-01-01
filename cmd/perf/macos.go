//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var (
	cmd = []string{
		"/usr/bin/powermetrics",
		"--samplers=gpu_power",
		"--sample-rate=500",
		"--sample-count=1",
	}

	ErrLineNotFound = errors.New("line with prefix not found")
)

type GPUUtilisation struct {
	ActiveFrequency uint64
	ActiveResidency float64
	IdleResidency   float64
	Power           float64
}

// IsRoot checks whether the current user is root.
func IsRoot() bool {
	uid := os.Getuid()

	return uid == 0
}

// GetGPUUtilisation gets the M1 GPU utilisation.
func GetGPUUtilisation() (*GPUUtilisation, error) {
	// Run cmd, and parse the results.
	cmdOutput, err := exec.Command(cmd[0], cmd[1:]...).Output()
	if err != nil {
		return nil, err
	}

	// Parse the output, which looks like:
	//
	// Machine model: Mac13,1
	// OS version: 23C71
	// Boot arguments:
	// Boot time: Wed Dec 27 19:42:05 2023

	// *** Sampled system activity (Mon Jan  1 14:42:26 2024 +0000) (503.45ms elapsed) ***

	// **** GPU usage ****

	// GPU HW active frequency: 638 MHz
	// GPU HW active residency:   2.21% (389 MHz: .09% 486 MHz:   0% 648 MHz: 2.1% 778 MHz:   0% 972 MHz:   0% 1296 MHz:   0%)
	// GPU SW requested state: (P1 :   0% P2 :   0% P3 : 100% P4 :   0% P5 :   0% P6 :   0%)
	// GPU SW state: (SW_P1 :   0% SW_P2 :   0% SW_P3 :   0% SW_P4 :   0% SW_P5 :   0% SW_P6 :   0%)
	// GPU idle residency:  97.79%
	// GPU Power: 16 mW

	var utilisation GPUUtilisation

	line, err := parseLine("GPU HW active frequency:", strings.Split(string(cmdOutput), "\n"))
	if err != nil {
		return nil, err
	}

	_, err = fmt.Sscanf(line, "%d MHz", &utilisation.ActiveFrequency)
	if err == nil {
		// Rescale to Hz.
		utilisation.ActiveFrequency *= 1_000_000
	} else {
		_, err = fmt.Sscanf(line, "%d GHz", &utilisation.ActiveFrequency)
		if err != nil {
			return nil, fmt.Errorf("failed to parse GPU HW active frequency: %w", err)
		}

		// Rescale to Hz.
		utilisation.ActiveFrequency *= 1_000_000_000
	}

	line, err = parseLine("GPU HW active residency:", strings.Split(string(cmdOutput), "\n"))
	if err != nil {
		return nil, err
	}

	_, err = fmt.Sscanf(line, "%f%%", &utilisation.ActiveResidency)
	if err != nil {
		return nil, fmt.Errorf("failed to parse GPU HW active residency: %w", err)
	}

	line, err = parseLine("GPU idle residency:", strings.Split(string(cmdOutput), "\n"))
	if err != nil {
		return nil, err
	}

	_, err = fmt.Sscanf(line, "%f%%", &utilisation.IdleResidency)
	if err != nil {
		return nil, fmt.Errorf("failed to parse GPU idle residency: %w", err)
	}

	line, err = parseLine("GPU Power:", strings.Split(string(cmdOutput), "\n"))
	if err != nil {
		return nil, err
	}

	_, err = fmt.Sscanf(line, "%f mW", &utilisation.Power)
	if err == nil {
		// Rescale to W.
		utilisation.Power /= 1_000
	} else {
		_, err = fmt.Sscanf(line, "%f W", &utilisation.Power)
		if err != nil {
			return nil, fmt.Errorf("failed to parse GPU Power: %w", err)
		}
	}

	return &utilisation, nil
}

func parseLine(prefix string, lines []string) (string, error) {
	// Find the line that starts with the prefix.
	for _, line := range lines {
		if trimmedLine, found := strings.CutPrefix(line, prefix); found {
			return trimmedLine, nil
		}
	}

	return "", fmt.Errorf("%w: %s", ErrLineNotFound, prefix)
}
