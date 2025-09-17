package blue_green_state_monitor_go

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type BlueGreenState struct {
	Current    NamespaceVersion
	Sibling    *NamespaceVersion
	UpdateTime time.Time
}

type NamespaceVersion struct {
	Namespace string
	Version   *Version
	State     State
}

type State string

const (
	StateActive    State = "ACTIVE"
	StateCandidate State = "CANDIDATE"
	StateLegacy    State = "LEGACY"
	StateIdle      State = "IDLE"
)

var statesShortNames = map[State]string{
	StateActive:    "a",
	StateCandidate: "c",
	StateLegacy:    "l",
	StateIdle:      "i",
}

func BGStateForNamespace(namespace string) BlueGreenState {
	return BGStateForCurrent(NamespaceVersion{
		Namespace: namespace,
		Version:   NewVersionMust("v1"),
		State:     StateActive,
	})
}

func BGStateForCurrent(currentNamespace NamespaceVersion) BlueGreenState {
	return BGStateForCurrentAndSibling(currentNamespace, nil)
}

func BGStateForCurrentAndSibling(currentNamespace NamespaceVersion, siblingNamespace *NamespaceVersion) BlueGreenState {
	return BlueGreenState{
		Current:    currentNamespace,
		Sibling:    siblingNamespace,
		UpdateTime: UnknownDatetime,
	}
}

func (s State) String() string {
	return string(s)
}

func (s State) ShortString() string {
	return statesShortNames[s]
}

func StateFromShort(shortName string) (State, error) {
	shortName = strings.ToLower(shortName)
	for k, v := range statesShortNames {
		if v == shortName {
			return k, nil
		}
	}
	return "", fmt.Errorf("unknown blue green status with short name: %s", shortName)
}

func StateFromName(name string) (State, error) {
	name = strings.ToUpper(name)
	for k := range statesShortNames {
		if k.String() == name {
			return k, nil
		}
	}
	return "", fmt.Errorf("unknown blue green status with name: %s", name)
}

var versionPattern = regexp.MustCompile(`v?(\d+)`)

type Version struct {
	Value    string
	IntValue int
}

func NewVersion(version string) (*Version, error) {
	if strings.TrimSpace(version) == "" {
		return &Version{
			Value:    "",
			IntValue: 0,
		}, nil
	} else {
		submatch := versionPattern.FindStringSubmatch(version)
		if submatch[1] != "" {
			if intV, err := strconv.Atoi(submatch[1]); err == nil {
				return &Version{
					Value:    "v" + submatch[1],
					IntValue: intV,
				}, nil
			} else {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("invalid version format, must match the following regexp: %s", versionPattern)
		}
	}
}

func NewVersionMust(version string) *Version {
	v, err := NewVersion(version)
	if err != nil {
		panic(err)
	}
	return v
}

func (v *Version) IsEmpty() bool {
	return v.Value == ""
}

func (v *Version) String() string {
	if v == nil {
		return "nil"
	} else {
		return v.Value
	}
}

func (v *Version) Equal(another *Version) bool {
	if v == another {
		return true
	}
	if v == nil || another == nil {
		return false
	} else {
		return v.Value == another.Value
	}
}

func (s *BlueGreenState) String() string {
	if s == nil {
		return "nil"
	} else {
		return fmt.Sprintf("Current: {%s}, Sibling: {%s}, UpdateTime: %s",
			s.Current.String(), s.Sibling.String(), s.UpdateTime.String())
	}
}

func (s *BlueGreenState) Equal(another *BlueGreenState) bool {
	if s == another {
		return true
	}
	if s == nil || another == nil {
		return false
	} else {
		return s.Current.Equal(&another.Current) &&
			s.Sibling.Equal(another.Sibling) &&
			s.UpdateTime.Equal(another.UpdateTime)
	}
}

func (nsV *NamespaceVersion) String() string {
	if nsV == nil {
		return "nil"
	} else {
		return fmt.Sprintf("Namespace: %s, State: %s, Version: %s",
			nsV.Namespace, nsV.State, nsV.Version.String())
	}
}

func (nsV *NamespaceVersion) Equal(another *NamespaceVersion) bool {
	if nsV == another {
		return true
	}
	if nsV == nil || another == nil {
		return false
	} else {
		return nsV.Namespace == another.Namespace &&
			nsV.Version.Equal(another.Version) &&
			nsV.State == another.State
	}
}
