/*
 * Copyright (c) 2023, NVIDIA CORPORATION.  All rights reserved.
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

package selector

import (
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Selector defines the set of conditions that can be used to select on a set of properties
// As a first level of nesting, either a single property can be selected or the
// conditions can be 'anded' or 'ored' together
// +k8s:deepcopy-gen=false
type Selector[T any] struct {
	Properties    *T
	AndExpression []Selector[T]
	OrExpression  []Selector[T]
}

// SelectorList is a list of Selectors
// +k8s:deepcopy-gen=false
type SelectorList[T any] []Selector[T]

// IntProperty defines an int type that methods can hang off of.
type IntProperty int

// StringProperty defines a string type that methods can hang off of.
type StringProperty string

// BoolProperty defines a bool type that methods can hang off of.
type BoolProperty bool

// GlobProperty defines a string type that can contain glob for matching against.
type GlobProperty string

// QuantityComparatorOperator defines the operators for use with a QuantityComparator.
// +kubebuilder:validation:Enum=Equals;LessThan;LessThanOrEqualTo;GreaterThan;GreaterThanOrEqualTo
type QuantityComparatorOperator string

// VersionComparatorOperator defines the operators for use with a VersionComparator.
// +kubebuilder:validation:Enum=Equals;LessThan;LessThanOrEqualTo;GreaterThan;GreaterThanOrEqualTo
type VersionComparatorOperator string

// QuantityComparator compares a quantity SelectorCondition using a specific operator.
type QuantityComparator struct {
	Value    resource.Quantity          `json:"value,omitempty"`
	Operator QuantityComparatorOperator `json:"operator,omitempty"`
}

// VersionComparator compares a version SelectorCondition using a specific operator.
type VersionComparator struct {
	Value    string                    `json:"value,omitempty"`
	Operator VersionComparatorOperator `json:"operator,omitempty"`
}

// Matches evaluates a Selector to see if it matches the boolean expression it represents.
// Each individual Properties object is passed to the caller via a callback to
// compare it in isolation before combining the results.
func (s Selector[T]) Matches(compare func(*T) bool) bool {
	if s.Properties != nil {
		return compare(s.Properties)
	}
	if s.AndExpression != nil {
		return SelectorList[T](s.AndExpression).And(compare)
	}
	if s.OrExpression != nil {
		return SelectorList[T](s.OrExpression).Or(compare)
	}
	return false
}

// And runs an 'and' operation between each element in SelectorList.
// Each individual Properties object is passed to the caller via a callback to
// compare it in isolation before combining the results.
func (l SelectorList[T]) And(compare func(*T) bool) bool {
	and := true
	for _, s := range l {
		and = and && s.Matches(compare)
	}
	return and
}

// Or runs an 'or' operation between each element in SelectorList.
// Each individual Properties object is passed to the caller via a callback to
// compare it in isolation before combining the results.
func (l SelectorList[T]) Or(compare func(*T) bool) bool {
	or := false
	for _, s := range l {
		or = or || s.Matches(compare)
	}
	return or
}

// Matches checks if the provided int matches the IntProperty.
func (p IntProperty) Matches(i int) bool {
	return int(p) == i
}

// Matches checks if the provided string matches the StringProperty.
func (p StringProperty) Matches(s string) bool {
	return string(p) == s
}

// Matches checks if the provided bool matches the BoolProperty.
func (p BoolProperty) Matches(b bool) bool {
	return bool(p) == b
}

// Matches checks if the provided string matches the GlobProperty.
func (g GlobProperty) Matches(s string) bool {
	lowerg := strings.ToLower(string(g))
	lowers := strings.ToLower(s)
	result, _ := regexp.MatchString(wildCardToRegexp(lowerg), lowers)
	return result
}

// Matches checks if 'version' matches the semantics of the QuantityComparator.
func (c *QuantityComparator) Matches(quantity *resource.Quantity) bool {
	compare := quantity.Cmp(c.Value)
	return checkCompareValue(compare, string(c.Operator))
}

// Matches checks if a 'version' matches the semantics of the VersionComparator.
func (c *VersionComparator) Matches(version string) bool {
	compare := semver.Compare(vVersion(version), vVersion(c.Value))
	return checkCompareValue(compare, string(c.Operator))
}

// vVersion prepends a 'v' to the version string if one is missing.
func vVersion(version string) string {
	vversion := version
	if vversion[0] != 'v' {
		vversion = "v" + version
	}
	return vversion
}

// checkCompareValue will check the result of a standard comparison operation
// against the string representation of the operation to see if they match.
func checkCompareValue(value int, operator string) bool {
	switch operator {
	case "Equals":
		return value == 0
	case "LessThan":
		return value == -1
	case "LessThanOrEqualTo":
		return value == 0 || value == -1
	case "GreaterThan":
		return value == 1
	case "GreaterThanOrEqualTo":
		return value == 0 || value == 1
	}
	return false
}

// wildCardToRegexp converts a wildcard pattern to a regular expression pattern.
func wildCardToRegexp(pattern string) string {
	var result strings.Builder
	for i, literal := range strings.Split(pattern, "*") {
		// Replace * with .*
		if i > 0 {
			result.WriteString(".*")
		}
		// Quote any regular expression meta characters in the literal text.
		result.WriteString(regexp.QuoteMeta(literal))
	}
	return result.String()
}
