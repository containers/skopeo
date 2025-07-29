package main

const (
	// TestImageManifestDigest is the Docker manifest digest of "fixtures/image.manifest.json"
	TestImageManifestDigest = "sha256:20bf21ed457b390829cdbeec8795a7bea1626991fda603e0d01b4e7f60427e55"

	testSequoiaHome = "./fixtures"
	// testSequoiaKeyFingerprint is a fingerprint of a test key in testSequoiaHome, generated using
	// > sq --home $(pwd)/signature/simplesequoia/testdata key generate --name 'Skopeo Sequoia testing key' --own-key --expiration=never
	testSequoiaKeyFingerprint = "50DDE898DF4E48755C8C2B7AF6F908B6FA48A229"
	// testSequoiaKeyFingerprintWithPassphrase is a fingerprint of a test key in testSequoiaHome, generated using
	// > sq --home $(pwd)/signature/simplesequoia/testdata key generate --name 'Skopeo Sequoia testing key with passphrase' --own-key --expiration=never
	testSequoiaKeyFingerprintWithPassphrase = "1F5825285B785E1DB13BF36D2D11A19ABA41C6AE"
)
