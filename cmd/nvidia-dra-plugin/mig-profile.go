/*
 * Copyright (c) 2021, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

const (
	// AttributeMediaExtensions holds the string representation for the media extension MIG profile attribute.
	AttributeMediaExtensions = "me"
)

// MigProfile represents a specific MIG profile.
// Examples include "1g.5gb", "2g.10gb", "1c.2g.10gb", or "1c.1g.5gb+me", etc.
type MigProfile struct {
	C              int
	G              int
	GB             int
	GIProfileID    int
	CIProfileID    int
	CIEngProfileID int
}

// NewMigProfile constructs a new MigProfile struct using info from the giProfiles and ciProfiles used to create it.
func NewMigProfile(giProfileID, ciProfileID, ciEngProfileID int, giSliceCount, ciSliceCount uint32, migMemorySizeMB, totalDeviceMemoryBytes uint64) *MigProfile {
	return &MigProfile{
		C:              int(ciSliceCount),
		G:              int(giSliceCount),
		GB:             int(getMigMemorySizeInGB(totalDeviceMemoryBytes, migMemorySizeMB)),
		GIProfileID:    giProfileID,
		CIProfileID:    ciProfileID,
		CIEngProfileID: ciEngProfileID,
	}
}

// ParseMigProfile converts a string representation of a MigProfile into an object.
func ParseMigProfile(profile string) (*MigProfile, error) {
	var err error
	var c, g, gb int
	var attr []string

	if len(profile) == 0 {
		return nil, fmt.Errorf("empty MigProfile string")
	}

	split := strings.SplitN(profile, "+", 2)
	if len(split) == 2 {
		attr, err = parseMigProfileAttributes(split[1])
		if err != nil {
			return nil, fmt.Errorf("error parsing attributes following '+' in MigProfile string: %v", err)
		}
	}

	c, g, gb, err = parseMigProfileFields(split[0])
	if err != nil {
		return nil, fmt.Errorf("error parsing '.' separated fields in MigProfile string: %v", err)
	}

	m := &MigProfile{
		C:  c,
		G:  g,
		GB: gb,
	}

	switch c {
	case 1:
		m.CIProfileID = nvml.COMPUTE_INSTANCE_PROFILE_1_SLICE
	case 2:
		m.CIProfileID = nvml.COMPUTE_INSTANCE_PROFILE_2_SLICE
	case 3:
		m.CIProfileID = nvml.COMPUTE_INSTANCE_PROFILE_3_SLICE
	case 4:
		m.CIProfileID = nvml.COMPUTE_INSTANCE_PROFILE_4_SLICE
	case 6:
		m.CIProfileID = nvml.COMPUTE_INSTANCE_PROFILE_6_SLICE
	case 7:
		m.CIProfileID = nvml.COMPUTE_INSTANCE_PROFILE_7_SLICE
	case 8:
		m.CIProfileID = nvml.COMPUTE_INSTANCE_PROFILE_8_SLICE
	default:
		return nil, fmt.Errorf("unknown Compute Instance slice size: %v", c)
	}

	switch g {
	case 1:
		m.GIProfileID = nvml.GPU_INSTANCE_PROFILE_1_SLICE
	case 2:
		m.GIProfileID = nvml.GPU_INSTANCE_PROFILE_2_SLICE
	case 3:
		m.GIProfileID = nvml.GPU_INSTANCE_PROFILE_3_SLICE
	case 4:
		m.GIProfileID = nvml.GPU_INSTANCE_PROFILE_4_SLICE
	case 6:
		m.GIProfileID = nvml.GPU_INSTANCE_PROFILE_6_SLICE
	case 7:
		m.GIProfileID = nvml.GPU_INSTANCE_PROFILE_7_SLICE
	case 8:
		m.GIProfileID = nvml.GPU_INSTANCE_PROFILE_8_SLICE
	default:
		return nil, fmt.Errorf("unknown GPU Instance slice size: %v", g)
	}

	m.CIEngProfileID = nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_SHARED

	for _, a := range attr {
		switch a {
		case AttributeMediaExtensions:
			m.GIProfileID = nvml.GPU_INSTANCE_PROFILE_1_SLICE_REV1
		default:
			return nil, fmt.Errorf("unknown MigProfile attribute: %v", a)
		}
	}

	return m, nil
}

// MustParseMigProfile does the same as Parse(), but never throws an error.
func MustParseMigProfile(profile string) *MigProfile {
	p, _ := ParseMigProfile(profile)
	return p
}

// Attributes returns the list of attributes associated with a MigProfile
func (m MigProfile) Attributes() []string {
	var attr []string
	switch m.GIProfileID {
	case nvml.GPU_INSTANCE_PROFILE_1_SLICE_REV1:
		attr = append(attr, AttributeMediaExtensions)
	}
	return attr
}

// HasAttribute checks if the MigProfile has the specified attribute associated with it.
func (m MigProfile) HasAttribute(attr string) bool {
	for _, a := range m.Attributes() {
		if a == attr {
			return true
		}
	}
	return false
}

// String returns the string representation of a MigProfile.
func (p MigProfile) String() string {
	var suffix string
	if len(p.Attributes()) > 0 {
		suffix = "+" + strings.Join(p.Attributes(), ",")
	}
	if p.C == p.G {
		return fmt.Sprintf("%dg.%dgb%s", p.G, p.GB, suffix)
	}
	return fmt.Sprintf("%dc.%dg.%dgb%s", p.C, p.G, p.GB, suffix)
}

// Equals checks if two MigProfiles are identical or not
func (m MigProfile) Equals(other *MigProfile) bool {
	return m == *other
}

func parseMigProfileField(s string, field string) (int, error) {
	if strings.TrimSpace(s) != s {
		return -1, fmt.Errorf("leading or trailing spaces on '%%d%s'", field)
	}

	if !strings.HasSuffix(s, field) {
		return -1, fmt.Errorf("missing '%s' from '%%d%s'", field, field)
	}

	v, err := strconv.Atoi(strings.TrimSuffix(s, field))
	if err != nil {
		return -1, fmt.Errorf("malformed number in '%%d%s'", field)
	}

	return v, nil
}

func parseMigProfileFields(s string) (int, int, int, error) {
	var err error
	var c, g, gb int

	split := strings.SplitN(s, ".", 3)
	if len(split) == 3 {
		c, err = parseMigProfileField(split[0], "c")
		if err != nil {
			return -1, -1, -1, err
		}
		g, err = parseMigProfileField(split[1], "g")
		if err != nil {
			return -1, -1, -1, err
		}
		gb, err = parseMigProfileField(split[2], "gb")
		if err != nil {
			return -1, -1, -1, err
		}
		return c, g, gb, err
	}
	if len(split) == 2 {
		g, err = parseMigProfileField(split[0], "g")
		if err != nil {
			return -1, -1, -1, err
		}
		gb, err = parseMigProfileField(split[1], "gb")
		if err != nil {
			return -1, -1, -1, err
		}
		return g, g, gb, nil
	}

	return -1, -1, -1, fmt.Errorf("parsed wrong number of fields, expected 2 or 3")
}

func parseMigProfileAttributes(s string) ([]string, error) {
	attr := strings.Split(s, ",")
	if len(attr) == 0 {
		return nil, fmt.Errorf("empty attribute list")
	}
	unique := make(map[string]int)
	for _, a := range attr {
		if unique[a] > 0 {
			return nil, fmt.Errorf("non unique attribute in list")
		}
		if a == "" {
			return nil, fmt.Errorf("empty attribute in list")
		}
		if strings.TrimSpace(a) != a {
			return nil, fmt.Errorf("leading or trailing spaces in attribute")
		}
		if a[0] >= '0' && a[0] <= '9' {
			return nil, fmt.Errorf("attribute begins with a number")
		}
		for _, c := range a {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return nil, fmt.Errorf("non alpha-numeric character or digit in attribute")
			}
		}
		unique[a]++
	}
	return attr, nil
}

func getMigMemorySizeInGB(totalDeviceMemory, migMemorySizeMB uint64) uint64 {
	const fracDenominator = 8
	const oneMB = 1024 * 1024
	const oneGB = 1024 * 1024 * 1024
	fractionalGpuMem := (float64(migMemorySizeMB) * oneMB) / float64(totalDeviceMemory)
	fractionalGpuMem = math.Ceil(fractionalGpuMem*fracDenominator) / fracDenominator
	totalMemGB := float64((totalDeviceMemory + oneGB - 1) / oneGB)
	return uint64(math.Round(fractionalGpuMem * totalMemGB))
}
