package main

const blockedRegistriesConf = "./fixtures/blocked-registries.conf"
const blockedErrorRegex = `.*registry registry-blocked.com is blocked in .*`

func (s *skopeoSuite) TestCopyBlockedSource() {
	t := s.T()
	assertSkopeoFails(t, blockedErrorRegex,
		"--registries-conf", blockedRegistriesConf, "copy",
		"docker://registry-blocked.com/image:test",
		"docker://registry-unblocked.com/image:test")
}

func (s *skopeoSuite) TestCopyBlockedDestination() {
	t := s.T()
	assertSkopeoFails(t, blockedErrorRegex,
		"--registries-conf", blockedRegistriesConf, "copy",
		"docker://registry-unblocked.com/image:test",
		"docker://registry-blocked.com/image:test")
}

func (s *skopeoSuite) TestInspectBlocked() {
	t := s.T()
	assertSkopeoFails(t, blockedErrorRegex,
		"--registries-conf", blockedRegistriesConf, "inspect",
		"docker://registry-blocked.com/image:test")
}

func (s *skopeoSuite) TestDeleteBlocked() {
	t := s.T()
	assertSkopeoFails(t, blockedErrorRegex,
		"--registries-conf", blockedRegistriesConf, "delete",
		"docker://registry-blocked.com/image:test")
}
