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
	C          int
	G          int
	GB         int
	Attributes []string
}

// NewMigProfile constructs a new MigProfile struct using info from the giProfiles and ciProfiles used to create it.
func NewMigProfile(giSlices, ciSlices uint32, attributes []string, migMemorySizeMB, deviceMemorySizeBytes uint64) *MigProfile {
	return &MigProfile{
		C:          int(ciSlices),
		G:          int(giSlices),
		GB:         int(getMigMemorySizeGB(deviceMemorySizeBytes, migMemorySizeMB)),
		Attributes: attributes,
	}
}

// GetMigProfileAttributes returns a list of attributes given a GpuInstanceProfile
func GetMigProfileAttributes(giProfileID int) []string {
	var attr []string
	switch giProfileID {
	case nvml.GPU_INSTANCE_PROFILE_1_SLICE_REV1:
		attr = append(attr, AttributeMediaExtensions)
	}
	return attr
}

// ParseMigProfile converts a string representation of a MigProfile into an object.
func ParseMigProfile(profile string) (*MigProfile, error) {
	var err error
	var c, g, gb int
	var attr []string

	if len(profile) == 0 {
		return nil, fmt.Errorf("empty Profile string")
	}

	split := strings.SplitN(profile, "+", 2)
	if len(split) == 2 {
		attr, err = parseProfileAttributes(split[1])
		if err != nil {
			return nil, fmt.Errorf("error parsing attributes following '+' in MigProfile string: %v", err)
		}
	}

	c, g, gb, err = parseProfileFields(split[0])
	if err != nil {
		return nil, fmt.Errorf("error parsing '.' separated fields in MigProfile string: %v", err)
	}

	p := &MigProfile{
		C:          c,
		G:          g,
		GB:         gb,
		Attributes: attr,
	}

	return p, nil
}

// MustParseMigProfile does the same as Parse(), but never throws an error.
func MustParseMigProfile(profile string) *MigProfile {
	p, _ := ParseMigProfile(profile)
	return p
}

// HasAttribute checks if the MigProfile has the specified attribute associated with it.
func (p MigProfile) HasAttribute(attr string) bool {
	for _, a := range p.Attributes {
		if a == attr {
			return true
		}
	}
	return false
}

// String returns the string representation of a MigProfile.
func (p MigProfile) String() string {
	var suffix string
	if len(p.Attributes) > 0 {
		suffix = "+" + strings.Join(p.Attributes, ",")
	}
	if p.C == p.G {
		return fmt.Sprintf("%dg.%dgb%s", p.G, p.GB, suffix)
	}
	return fmt.Sprintf("%dc.%dg.%dgb%s", p.C, p.G, p.GB, suffix)
}

// Equals checks if two MigProfiles are identical or not
func (p MigProfile) Equals(other *MigProfile) bool {
	if p.C != other.C {
		return false
	}
	if p.G != other.G {
		return false
	}
	if p.GB != other.GB {
		return false
	}
	if len(p.Attributes) != len(other.Attributes) {
		return false
	}
	for _, a := range p.Attributes {
		if !other.HasAttribute(a) {
			return false
		}
	}
	return true
}

func parseProfileField(s string, field string) (int, error) {
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

func parseProfileFields(s string) (int, int, int, error) {
	var err error
	var c, g, gb int

	split := strings.SplitN(s, ".", 3)
	if len(split) == 3 {
		c, err = parseProfileField(split[0], "c")
		if err != nil {
			return -1, -1, -1, err
		}
		g, err = parseProfileField(split[1], "g")
		if err != nil {
			return -1, -1, -1, err
		}
		gb, err = parseProfileField(split[2], "gb")
		if err != nil {
			return -1, -1, -1, err
		}
		return c, g, gb, err
	}
	if len(split) == 2 {
		g, err = parseProfileField(split[0], "g")
		if err != nil {
			return -1, -1, -1, err
		}
		gb, err = parseProfileField(split[1], "gb")
		if err != nil {
			return -1, -1, -1, err
		}
		return g, g, gb, nil
	}

	return -1, -1, -1, fmt.Errorf("parsed wrong number of fields, expected 2 or 3")
}

func parseProfileAttributes(s string) ([]string, error) {
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

func getMigMemorySizeGB(totalDeviceMemory, migMemorySizeMB uint64) uint64 {
	const fracDenominator = 8
	const oneMB = 1024 * 1024
	const oneGB = 1024 * 1024 * 1024
	fractionalGpuMem := (float64(migMemorySizeMB) * oneMB) / float64(totalDeviceMemory)
	fractionalGpuMem = math.Ceil(fractionalGpuMem*fracDenominator) / fracDenominator
	totalMemGB := float64((totalDeviceMemory + oneGB - 1) / oneGB)
	return uint64(math.Round(fractionalGpuMem * totalMemGB))
}
