//go:build darwin

package hostx

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/weshofmann/boxwarden/internal/execx"
)

var localDirectoryEnvironment = []string{"LC_ALL=C", "LANG=C"}

func inspectExactLocalOperatorGroup(runner execx.Runner, caller Operator, name string, allowEmpty bool) (Group, error) {
	if runner == nil || name != OperatorGroupName || caller.UID <= 0 || !validDirectoryRecordName(caller.Name) {
		return Group{}, fmt.Errorf("invalid exact local operator group request")
	}
	groupAttributes, err := readLocalDirectoryAttributes(runner, []string{"/Local/Default", "-read", "/Groups/" + name})
	if err != nil {
		return Group{}, err
	}
	recordName, ok := exactAttribute(groupAttributes, "RecordName")
	if !ok || recordName != name {
		return Group{}, fmt.Errorf("local operator group has invalid record name")
	}
	rawGID, ok := exactAttribute(groupAttributes, "PrimaryGroupID")
	if !ok {
		return Group{}, fmt.Errorf("local operator group has no exact primary gid")
	}
	gid, err := strconv.Atoi(rawGID)
	if err != nil || gid < 0 {
		return Group{}, fmt.Errorf("local operator group has invalid primary gid")
	}
	if nested := groupAttributes["NestedGroups"]; len(nested) != 0 {
		return Group{}, fmt.Errorf("local operator group has nested membership")
	}

	userAttributes, err := readLocalDirectoryAttributes(runner, []string{"/Local/Default", "-read", "/Users/" + caller.Name, "RecordName", "UniqueID", "GeneratedUID"})
	if err != nil {
		return Group{}, err
	}
	nameOK := hasUniqueCallerRecordName(userAttributes, caller.Name)
	rawUID, uidOK := exactAttribute(userAttributes, "UniqueID")
	generatedUID, guidOK := exactAttribute(userAttributes, "GeneratedUID")
	uid, uidErr := strconv.Atoi(rawUID)
	if !nameOK || !uidOK || !guidOK || uidErr != nil || uid != caller.UID || generatedUID == "" {
		return Group{}, fmt.Errorf("local operator record does not exactly bind caller")
	}

	named, namedPresent := groupAttributes["GroupMembership"]
	numeric, numericPresent := groupAttributes["GroupMembers"]
	empty := len(named) == 0 && len(numeric) == 0
	exact := namedPresent && numericPresent && len(named) == 1 && named[0] == caller.Name && len(numeric) == 1 && numeric[0] == generatedUID
	if !exact && !(allowEmpty && empty) {
		return Group{}, fmt.Errorf("local operator group membership is not exact")
	}
	if empty && (namedPresent != numericPresent) {
		return Group{}, fmt.Errorf("local operator group membership attributes are incomplete")
	}

	primaryUsers, err := listDirectoryPrimaryGIDs(runner, []string{"/Search", "-list", "/Users", "PrimaryGroupID"})
	if err != nil {
		return Group{}, err
	}
	if _, present := primaryUsers[caller.Name]; !present {
		return Group{}, fmt.Errorf("caller is absent from exhaustive local user listing")
	}
	for candidate, primaryGID := range primaryUsers {
		if primaryGID == gid && candidate != caller.Name {
			return Group{}, fmt.Errorf("another local user has the operator group as primary gid")
		}
	}
	groupGIDs, err := listDirectoryPrimaryGIDs(runner, []string{"/Search", "-list", "/Groups", "PrimaryGroupID"})
	if err != nil {
		return Group{}, err
	}
	if listedGID, present := groupGIDs[name]; !present || listedGID != gid {
		return Group{}, fmt.Errorf("operator group gid is absent or aliased")
	}
	for candidate, candidateGID := range groupGIDs {
		if candidateGID == gid && candidate != name {
			return Group{}, fmt.Errorf("operator group gid is absent or aliased")
		}
	}
	group := Group{ID: gid, Name: name}
	if exact {
		group.Members = []int{caller.UID}
	}
	return group, nil
}

func readLocalDirectoryAttributes(runner execx.Runner, args []string) (map[string][]string, error) {
	output, err := runLocalDirectoryQuery(runner, args)
	if err != nil {
		return nil, err
	}
	attributes := map[string][]string{}
	current := ""
	for _, raw := range strings.Split(output, "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if len(raw) > 0 && unicode.IsSpace(rune(raw[0])) {
			if current == "" {
				return nil, fmt.Errorf("malformed local directory continuation")
			}
			attributes[current] = append(attributes[current], strings.Fields(raw)...)
			continue
		}
		key, value, found := strings.Cut(raw, ":")
		if !found || key == "" || strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("malformed local directory attribute")
		}
		if _, duplicate := attributes[key]; duplicate {
			return nil, fmt.Errorf("duplicate local directory attribute")
		}
		attributes[key] = strings.Fields(value)
		current = key
	}
	return attributes, nil
}

func listDirectoryPrimaryGIDs(runner execx.Runner, args []string) (map[string]int, error) {
	output, err := runLocalDirectoryQuery(runner, args)
	if err != nil {
		return nil, err
	}
	records := map[string]int{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		gid, conversionErr := strconv.Atoi(fields[len(fields)-1])
		if len(fields) != 2 || conversionErr != nil || gid < 0 || !validDirectoryRecordName(fields[0]) {
			return nil, fmt.Errorf("malformed local directory primary gid listing")
		}
		if _, duplicate := records[fields[0]]; duplicate {
			return nil, fmt.Errorf("duplicate local directory primary gid listing")
		}
		records[fields[0]] = gid
	}
	return records, nil
}

func runLocalDirectoryQuery(runner execx.Runner, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runner.Run(ctx, execx.Command{Path: "/usr/bin/dscl", Args: append([]string(nil), args...), Env: append([]string(nil), localDirectoryEnvironment...)})
	if err != nil || result.Truncated {
		return "", fmt.Errorf("inspect exact local directory state")
	}
	return result.Stdout, nil
}

func exactAttribute(attributes map[string][]string, key string) (string, bool) {
	values, ok := attributes[key]
	returnValue := ""
	if ok && len(values) == 1 {
		returnValue = values[0]
		return returnValue, true
	}
	return "", false
}

func hasUniqueCallerRecordName(attributes map[string][]string, callerName string) bool {
	values, present := attributes["RecordName"]
	if !present || len(values) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	callerCount := 0
	for _, value := range values {
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
		if value == callerName {
			callerCount++
		}
	}
	return callerCount == 1
}

func validDirectoryRecordName(name string) bool {
	if name == "" || len(name) > 255 {
		return false
	}
	for _, character := range name {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '.' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}
