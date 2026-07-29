package hazmat

import darwincompiler "hazmat/containment/darwin"

func compileDarwinSBPLChecked(policy nativeSessionPolicy) (string, error) {
	return darwincompiler.Compile(policy.Contract, darwincompiler.CompileOptions{
		MacOSSecurityFramework:       policy.MacOSSecurityFramework,
		MacOSAgentKeychainReadAccess: policy.MacOSAgentKeychainReadAccess,
		MacOSAgentKeychainAccess:     policy.MacOSAgentKeychainAccess,
		RuntimeTempDirs:              append([]string(nil), policy.RuntimeTempDirs...),
	})
}
