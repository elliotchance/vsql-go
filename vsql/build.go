package vsql

import (
	"errors"
	"sync"
)

// BuildInfo describes the build of libvsql that's compiled into the package,
// from SQLGetInfo.
type BuildInfo struct {
	// Version is the vsql release, such as "v1.7.58", or "" if libvsql wasn't
	// built from a release.
	Version string

	// Commit is the vsql git commit, or "" if libvsql wasn't built from a
	// release.
	Commit string

	// Date is the day the release was built, such as "2026-11-12" (UTC).
	Date string

	// VVersion and VCommit are the version (such as "0.5.2") and git commit
	// of the V compiler that generated libvsql.
	VVersion string
	VCommit  string

	// CCompiler is the C compiler that V used, such as "clang".
	CCompiler string

	// OS and Machine are the operating system (such as "macos") and machine
	// (such as "arm64") that libvsql was built for.
	OS      string
	Machine string
}

// Build is the build of libvsql that's compiled into the package.
func Build() (BuildInfo, error) {
	return build()
}

var build = sync.OnceValues(readBuild)

func readBuild() (BuildInfo, error) {
	c, err := connect(":memory:")
	if err != nil {
		return BuildInfo{}, err
	}

	var info BuildInfo
	var errs []error
	for _, field := range []struct {
		infoType int
		value    *string
	}{
		{infoBuildVersion, &info.Version},
		{infoBuildCommit, &info.Commit},
		{infoBuildDate, &info.Date},
		{infoBuildVVersion, &info.VVersion},
		{infoBuildVCommit, &info.VCommit},
		{infoBuildCCompiler, &info.CCompiler},
		{infoOperatingSystem, &info.OS},
		{infoMachine, &info.Machine},
	} {
		var err error
		*field.value, err = getInfoString(c.dbc, field.infoType)
		errs = append(errs, err)
	}
	errs = append(errs, c.Close())
	if err := errors.Join(errs...); err != nil {
		return BuildInfo{}, err
	}

	return info, nil
}
