export ARCHS=linux/amd64 linux/arm darwin/amd64

ci:
	curl -sSLo golang.sh https://raw.githubusercontent.com/Luzifer/github-publish/master/golang.sh
	bash golang.sh

fix-deps:
	/usr/bin/ag -l 'github.com/Sirupsen/logrus' vendor | xargs sed -i 's/Sirupsen/sirupsen/g'
