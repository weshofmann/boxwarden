package contract

const NativeInputSHA = "73cc3b2f38d978b9ab63046c89eea610ff26ab467f2353e064bdcfa18465f36e"
const SDKPath = "/Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk"
const SDKCanonicalName = "macosx27.0"
const PythonAlias = "/Applications/Xcode.app/Contents/Developer/usr/bin/python3"
const nativeToolRoot = "/Applications/Xcode.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/bin"
const rustToolRoot = "/Users/devel/Backup/boxwarden_archive/n1-diagnostic-20260930/build-inputs/toolchain/bin"

type ToolInput struct {
	Role string `json:"role"`
	Kind string `json:"kind"`
	Path string `json:"path"`
	SHA  string `json:"sha"`
}
type BuildInputs struct {
	Version          int           `json:"version"`
	NativeInputSHA   string        `json:"native_input_sha"`
	SDKPath          string        `json:"sdk_path"`
	SDKCanonicalName string        `json:"sdk_canonical_name"`
	PythonAlias      string        `json:"python_alias"`
	Inputs           [11]ToolInput `json:"inputs"`
}

func ToolInputPolicy(index int) ToolInput {
	switch index {
	case 0:
		return ToolInput{"go", "executable", "/Users/devel/.local/share/mise/installs/go/1.27.0/bin/go", ""}
	case 1:
		return ToolInput{"rustc", "executable", rustToolRoot + "/rustc", ""}
	case 2:
		return ToolInput{"cargo", "executable", rustToolRoot + "/cargo", ""}
	case 3:
		return ToolInput{"clang", "executable", nativeToolRoot + "/clang", ""}
	case 4:
		return ToolInput{"ar", "executable", nativeToolRoot + "/ar", ""}
	case 5:
		return ToolInput{"ld", "executable", nativeToolRoot + "/ld", ""}
	case 6:
		return ToolInput{"python3", "launcher", "/usr/bin/python3", ""}
	case 7:
		return ToolInput{"SDKSettings.json", "sdk-metadata", SDKPath + "/SDKSettings.json", ""}
	case 8:
		return ToolInput{"SDKSettings.plist", "sdk-metadata", SDKPath + "/SDKSettings.plist", ""}
	case 9:
		return ToolInput{"SystemVersion.plist", "sdk-metadata", SDKPath + "/System/Library/CoreServices/SystemVersion.plist", ""}
	case 10:
		return ToolInput{"python3-interpreter", "executable", "/Applications/Xcode.app/Contents/Developer/Library/Frameworks/Python3.framework/Versions/3.9/bin/python3.9", ""}
	}
	return ToolInput{}
}
func (b BuildInputs) Valid() bool {
	if b.Version != 1 || b.NativeInputSHA != NativeInputSHA || b.SDKPath != SDKPath || b.SDKCanonicalName != SDKCanonicalName || b.PythonAlias != PythonAlias {
		return false
	}
	for i, x := range b.Inputs {
		p := ToolInputPolicy(i)
		if x.Role != p.Role || x.Kind != p.Kind || x.Path != p.Path || !digest(x.SHA) {
			return false
		}
	}
	return true
}
func ParseBuildInputs(raw []byte, sha string) (BuildInputs, error) {
	var b BuildInputs
	if SHA(raw) != sha || strict(raw, MaxLockBytes, &b) != nil || !b.Valid() {
		return BuildInputs{}, ErrRefused
	}
	return b, nil
}
