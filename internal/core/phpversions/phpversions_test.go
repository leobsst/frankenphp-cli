package phpversions

import "testing"

func TestValidate(t *testing.T) {
	for _, v := range Supported {
		if err := Validate(v); err != nil {
			t.Errorf("Validate(%s) = %v", v, err)
		}
	}
	// An unsupported version is a plain error message, not a crash
	err := Validate("7.4")
	if err == nil || err.Error() != "Unsupported PHP version '7.4'. Supported: 8.2, 8.3, 8.4, 8.5" {
		t.Errorf("Validate(7.4) = %v", err)
	}
}

func TestResolveDefault(t *testing.T) {
	for in, want := range map[string]string{"": "8.3", "8.5": "8.5", "9.0": "8.3", " 8.4": "8.3"} {
		if got := ResolveDefault(in); got != want {
			t.Errorf("ResolveDefault(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveDistro(t *testing.T) {
	for in, want := range map[string]string{"": "alpine", " Trixie ": "trixie", "ALPINE": "alpine"} {
		if got, err := ResolveDistro(in); err != nil || got != want {
			t.Errorf("ResolveDistro(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ResolveDistro(" debian"); err == nil ||
		err.Error() != "Unsupported PHP_IMAGE_DISTRO ' debian'. Supported: alpine, trixie" {
		t.Errorf("debian: %v", err)
	}
}

func TestNamesAndPorts(t *testing.T) {
	if tag, err := ImageTag("8.4", "trixie"); err != nil || tag != "1-php8.4-trixie" {
		t.Errorf("ImageTag = %q, %v", tag, err)
	}
	if _, err := ImageTag("7.4", "alpine"); err == nil {
		t.Error("ImageTag accepted 7.4")
	}
	if ContainerName("8.3") != "frankenphp-8.3" || ImageName("8.3") != "custom-frankenphp:8.3" {
		t.Error("container or image name")
	}
	want := map[string][2]int{"8.2": {8280, 8243}, "8.3": {8380, 8343}, "8.4": {8480, 8443}, "8.5": {8580, 8543}}
	for v, ports := range want {
		if h, s, err := InternalPorts(v); err != nil || h != ports[0] || s != ports[1] {
			t.Errorf("InternalPorts(%s) = %d, %d, %v", v, h, s, err)
		}
	}
	if _, _, err := InternalPorts("7.4"); err == nil {
		t.Error("InternalPorts accepted 7.4")
	}
}

func TestDeprecationWarning(t *testing.T) {
	want := "PHP 8.2 is deprecated and will be removed in a future release. Consider upgrading to PHP 8.3 or later."
	if got := DeprecationWarning("8.2"); got != want {
		t.Errorf("8.2: %q", got)
	}
	if got := DeprecationWarning("8.4"); got != "" {
		t.Errorf("8.4: %q", got)
	}
}
