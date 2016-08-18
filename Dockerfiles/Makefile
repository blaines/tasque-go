TASQUE_VERSION=0.02
LANGUAGES=node

build:
	for language in $(LANGUAGES); do \
		TASQUE_VERSION=$(TASQUE_VERSION) \
		LANGUAGE=$$language \
		make -C $$language; \
	done

push: build
	for language in $(LANGUAGES); do \
		TASQUE_VERSION=$(TASQUE_VERSION) \
		LANGUAGE=$$language \
		make push -C $$language; \
	done
