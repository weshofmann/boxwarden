package exportx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/weshofmann/boxwarden/internal/execx"
)

// A complete token walk rejects duplicate keys at every depth before the
// manifest or entitlement document is decoded into a map.
func rejectDuplicateBundleJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := walkBundleJSON(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("inspector JSON has trailing data: %v", err)
	}
	return nil
}

func walkBundleJSON(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	start, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if start != '{' && start != '[' {
		return fmt.Errorf("unexpected inspector JSON delimiter")
	}
	seen := map[string]bool{}
	for decoder.More() {
		if start == '{' {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return fmt.Errorf("inspector JSON has duplicate or invalid key")
			}
			seen[key] = true
		}
		if err := walkBundleJSON(decoder); err != nil {
			return err
		}
	}
	end, err := decoder.Token()
	if err != nil {
		return err
	}
	if start == '{' && end != json.Delim('}') || start == '[' && end != json.Delim(']') {
		return fmt.Errorf("inspector JSON delimiter mismatch")
	}
	return nil
}

// The original compressed initrd must be an exact prefix. The only appended
// bytes permitted are the deterministic cpio entries produced by our packer.
func inspectPackedInspectorInitrd(root string, files map[string]string) error {
	original := filepath.Join(root, "casper", "initrd")
	packed := filepath.Join(root, "inspector-initrd")
	base, err := os.OpenFile(original, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer base.Close()
	image, err := os.OpenFile(packed, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer image.Close()
	baseInfo, err := base.Stat()
	if err != nil {
		return err
	}
	imageInfo, err := image.Stat()
	if err != nil || imageInfo.Size() <= baseInfo.Size() {
		return fmt.Errorf("inspector initrd lacks appended probe")
	}
	baseHash, imageHash := sha256.New(), sha256.New()
	if _, err := io.CopyN(baseHash, base, baseInfo.Size()); err != nil {
		return err
	}
	if _, err := io.CopyN(imageHash, image, baseInfo.Size()); err != nil {
		return err
	}
	if !bytes.Equal(baseHash.Sum(nil), imageHash.Sum(nil)) {
		return fmt.Errorf("inspector initrd changes original source prefix")
	}
	probe, err := os.ReadFile(filepath.Join(root, "alpha-probe"))
	if err != nil {
		return err
	}
	request, err := os.ReadFile(filepath.Join(root, "request.json"))
	if err != nil {
		return err
	}
	var expected bytes.Buffer
	expected.Write(bytes.Repeat([]byte{0}, int((-baseInfo.Size())&3)))
	writeCPIOEntry(&expected, "alpha-probe", probe, 1, 0o100755)
	writeCPIOEntry(&expected, "alpha-export-request.json", request, 2, 0o100400)
	writeCPIOEntry(&expected, "TRAILER!!!", nil, 3, 0)
	if int64(expected.Len()) != imageInfo.Size()-baseInfo.Size() {
		return fmt.Errorf("inspector initrd has unexpected appended length")
	}
	actual := make([]byte, expected.Len())
	if _, err := io.ReadFull(image, actual); err != nil || !bytes.Equal(actual, expected.Bytes()) {
		return fmt.Errorf("inspector initrd appended entries differ: %v", err)
	}
	if _, err := image.Read(make([]byte, 1)); err != io.EOF {
		return fmt.Errorf("inspector initrd has trailing data")
	}
	if files["casper/initrd"] == "" || files["inspector-initrd"] == "" {
		return fmt.Errorf("inspector initrd lacks manifest digests")
	}
	return nil
}

func writeCPIOEntry(output *bytes.Buffer, name string, data []byte, inode, mode int) {
	fields := []int{inode, mode, 0, 0, 1, 0, len(data), 0, 0, 0, 0, len(name) + 1, 0}
	output.WriteString("070701")
	for _, field := range fields {
		fmt.Fprintf(output, "%08x", field)
	}
	output.WriteString(name)
	output.WriteByte(0)
	output.Write(bytes.Repeat([]byte{0}, (-(110 + len(name) + 1))&3))
	output.Write(data)
	output.Write(bytes.Repeat([]byte{0}, (-len(data))&3))
}

func inspectInspectorSignature(ctx context.Context, runner execx.Runner, helper string) error {
	run := func(path string, args []string, stdin []byte) (execx.Result, error) {
		result, err := runner.Run(ctx, execx.Command{Path: path, Args: args,
			Env: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}, Stdin: stdin})
		if err != nil || result.Truncated {
			return execx.Result{}, fmt.Errorf("inspector signature check failed or overflowed: %v", err)
		}
		return result, nil
	}
	if _, err := run("/usr/bin/codesign", []string{"--verify", "--strict", helper}, nil); err != nil {
		return err
	}
	entitlements, err := run("/usr/bin/codesign", []string{"-d", "--entitlements", ":-", helper}, nil)
	if err != nil {
		return err
	}
	converted, err := run("/usr/bin/plutil", []string{"-convert", "json", "-o", "-", "-"}, []byte(entitlements.Stdout))
	if err != nil {
		return err
	}
	if err := rejectDuplicateBundleJSON([]byte(converted.Stdout)); err != nil {
		return err
	}
	var actual map[string]bool
	if err := json.Unmarshal([]byte(converted.Stdout), &actual); err != nil || len(actual) != 1 || !actual["com.apple.security.virtualization"] {
		return fmt.Errorf("inspector helper has unexpected entitlements")
	}
	return nil
}
