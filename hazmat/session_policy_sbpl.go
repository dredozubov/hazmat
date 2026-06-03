package hazmat

import darwincompiler "hazmat/containment/darwin"

// compileDarwinSBPL preserves the legacy package-main call shape while the
// actual SBPL compiler lives in containment/darwin.
func compileDarwinSBPL(policy nativeSessionPolicy) string {
	sbpl, err := compileDarwinSBPLChecked(policy)
	if err != nil {
		panic(err)
	}
	return sbpl
}

func compileDarwinSBPLChecked(policy nativeSessionPolicy) (string, error) {
	return darwincompiler.Compile(policy.Contract, darwincompiler.CompileOptions{
		MacOSSecurityFramework:   policy.MacOSSecurityFramework,
		MacOSAgentKeychainAccess: policy.MacOSAgentKeychainAccess,
		RuntimeTempDirs:          append([]string(nil), policy.RuntimeTempDirs...),
	})
}
