# Contributing to Skopeo

We'd love to have you join the community! Below summarizes the processes
that we follow.

## Topics

* [Reporting Issues](#reporting-issues)
* [Submitting Pull Requests](#submitting-pull-requests)
* [Communications](#communications)
<!--
* [Becoming a Maintainer](#becoming-a-maintainer)
-->

## Reporting Issues

Before reporting an issue, check our backlog of 
[open issues](https://github.com/containers/skopeo/issues)
to see if someone else has already reported it. If so, feel free to add
your scenario, or additional information, to the discussion. Or simply 
"subscribe" to it to be notified when it is updated.

If you find a new issue with the project we'd love to hear about it! The most
important aspect of a bug report is that it includes enough information for
us to reproduce it. So, please include as much detail as possible and try
to remove the extra stuff that doesn't really relate to the issue itself.
The easier it is for us to reproduce it, the faster it'll be fixed!

Please don't include any private/sensitive information in your issue!

## Submitting Pull Requests

No Pull Request (PR) is too small! Typos, additional comments in the code,
new testcases, bug fixes, new features, more documentation, ... it's all 
welcome!

While bug fixes can first be identified via an "issue", that is not required.
It's ok to just open up a PR with the fix, but make sure you include the same
information you would have included in an issue - like how to reproduce it.

PRs for new features should include some background on what use cases the
new code is trying to address. When possible and when it makes sense, try to break-up
larger PRs into smaller ones - it's easier to review smaller
code changes. But only if those smaller ones make sense as stand-alone PRs.

Regardless of the type of PR, all PRs should include:
* well documented code changes
* additional testcases. Ideally, they should fail w/o your code change applied
* documentation changes

Squash your commits into logical pieces of work that might want to be reviewed
separate from the rest of the PRs. Ideally, each commit should implement a single
idea, and the PR branch should pass the tests at every commit.  GitHub makes it easy
to review the cumulative effect of many commits; so, when in doubt, use smaller commits.

PRs that fix issues should include a reference like `Closes #XXXX` in the
commit message so that github will automatically close the referenced issue
when the PR is merged.

<!--
All PRs require at least two LGTMs (Looks Good To Me) from maintainers.
-->

### Sign your PRs

The sign-off is a line at the end of the explanation for the patch. Your
signature certifies that you wrote the patch or otherwise have the right to pass
it on as an open-source patch. The rules are simple: if you can certify
the below (from [developercertificate.org](http://developercertificate.org/)):

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.
660 York Street, Suite 102,
San Francisco, CA 94110 USA

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.

Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

Then you just add a line to every git commit message:

    Signed-off-by: Joe Smith <joe.smith@email.com>

Use your real name (sorry, no pseudonyms or anonymous contributions.)

If you set your `user.name` and `user.email` git configs, you can sign your
commit automatically with `git commit -s`.

### Dependencies management

Dependencies are managed via [standard go modules](https://golang.org/ref/mod).

In order to add a new dependency to this project:

- use `go get -d path/to/dep@version` to add a new line to `go.mod`
- run `make vendor`

In order to update an existing dependency:

- use `go get -d -u path/to/dep@version` to update the relevant dependency line in `go.mod`
- run `make vendor`

When new PRs for [containers/container-libs](https://github.com/containers/container-libs) break `skopeo` (i.e. `containers/container-libs` tests fail in `make test-skopeo`):

- create out a new branch in your `skopeo` checkout and switch to it
- find out the version of `containers/container-libs` you want to use and note its commit ID. You might also want to use a fork of `containers/container-libs`, in that case note its repo
- use `go get -d github.com/$REPO/container-libs/image/v5@$COMMIT_ID` to download the right version. The command will fetch the dependency and then fail because of a conflict in `go.mod`, this is expected. Note the pseudo-version (eg. `v5.13.1-0.20210707123201-50afbf0a326`)
- use `go mod edit -replace=go.podman.io/image/v5=github.com/$REPO/container-libs/image/v5@$PSEUDO_VERSION` to add a replacement line to `go.mod` (e.g. `replace go.podman.io/image/v5 => github.com/moio/container-libs/image/v5 v5.13.1-0.20210707123201-50afbf0a3262`)
- run `make vendor`
- make any other necessary changes in the skopeo repo (e.g. add other dependencies now required by `containers/container-libs`, or update skopeo for changed `containers/container-libs` API)
- optionally add new integration tests to the skopeo repo
- submit the resulting branch as a skopeo PR, marked “DO NOT MERGE”
- iterate until tests pass and the PR is reviewed
- then the original `containers/container-libs` PR can be merged, disregarding its `make test-skopeo` failure
- as soon as possible after that, in the skopeo PR, use `go mod edit -dropreplace=go.podman.io/image/v5` to remove the `replace` line in `go.mod`
- run `make vendor`
- update the skopeo PR with the result, drop the “DO NOT MERGE” marking
- after tests complete successfully again, merge the skopeo PR

## Communications

For general questions, or discussions, please use the
[#podman](https://app.slack.com/client/T08PSQ7BQ/C08MXJLCFCN) channel on the [CNCF
Slack](https://cloud-native.slack.com).

For development related discussions, please use the
[#podman-dev](https://app.slack.com/client/T08PSQ7BQ/C08NTKCDC1W) channel on the CNCF
Slack.

For discussions around issues/bugs and features, you can use the github
[issues](https://github.com/containers/skopeo/issues)
and
[PRs](https://github.com/containers/skopeo/pulls)
tracking system.

<!--
## Becoming a Maintainer

To become a maintainer you must first be nominated by an existing maintainer.
If a majority (>50%) of maintainers agree then the proposal is adopted and
you will be added to the list.

Removing a maintainer requires at least 75% of the remaining maintainers
approval, or if the person requests to be removed then it is automatic.
Normally, a maintainer will only be removed if they are considered to be
inactive for a long period of time or are viewed as disruptive to the community.

The current list of maintainers can be found in the 
[MAINTAINERS](MAINTAINERS) file.
-->
