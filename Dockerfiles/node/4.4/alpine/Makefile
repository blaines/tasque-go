build:
	cp ../../../../tasque ./
	docker build --force-rm --cpu-quota=1800000 --cpuset-cpus="0-7" --cpu-shares=102400 --memory=50GB \
							 -t tasque/node:$(VERSION)-$(TASQUE_VERSION)-$(VARIANT) \
							 -t tasque/node:$(VERSION)-$(VARIANT) \
							 -t tasque/node:$(VARIANT) \
							  .
	rm tasque

push:
	docker push tasque/node:$(VERSION)-$(TASQUE_VERSION)-$(VARIANT)
	docker push tasque/node:$(VARIANT)
