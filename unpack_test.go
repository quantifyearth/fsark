package main

import "testing"

func TestDigestFromConfig(t *testing.T) {
	testcases := []struct {
		Config string
		Digest string
	}{
		{
			"sha256:8c0e80291942fb3a7da0fd26615468f5458f973538ba5e9a9f566c36da0159d0",
			"8c0e80291942fb3a7da0fd26615468f5458f973538ba5e9a9f566c36da0159d0",
		},
		{
			"9c7a54a9a43cca047013b82af109fe963fde787f63f9e016fdc3384500c2823d.json",
			"9c7a54a9a43cca047013b82af109fe963fde787f63f9e016fdc3384500c2823d",
		},
		{
			"b463e175e7733889069dc4e2df6004b1b1db91b702f301fcc4cb1542bec78f20",
			"b463e175e7733889069dc4e2df6004b1b1db91b702f301fcc4cb1542bec78f20",
		},
	}
	for _, testcase := range testcases {
		config := imageManifestItem{Config: testcase.Config}
		if config.Digest() != testcase.Digest {
			t.Errorf("Exepcted %s, got %s", testcase.Digest, config.Digest())
		}
	}
}
