FROM registry.fedoraproject.org/fedora:latest

ENV GOPATH="/var/tmp/go"
ENV GOSRC="$GOPATH/src/github.com/containers/skopeo"
ENV PATH="$GOPATH/bin:$GOSRC/bin:/usr/share/gocode/bin:$PATH"

COPY ./.container_packages $GOSRC/
RUN dnf -y update \
	&& dnf -y install $(sed -r -e '/^#/d' -e '/^$/d' $GOSRC/.container_packages) \
	&& dnf -y upgrade \
	&& dnf erase -y skopeo \
	&& dnf clean all

COPY ./hack/test_env_setup.sh $GOSRC/hack/
RUN bash $GOSRC/hack/test_env_setup.sh \
	&& useradd testuser \
	&& chown -R testuser:testuser $GOPATH
USER testuser
COPY . $GOSRC
WORKDIR $GOSRC
