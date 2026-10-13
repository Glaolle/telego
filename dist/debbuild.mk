# Build the Debian amd64 package. Keep the staging files for inspection.
DEBBUILD_DIST := $(MAKEFILE_DIR)dist
DEBBUILD_DIR := $(MAKEFILE_DIR).build/debbuild
DEBBUILD_VERSION := $(patsubst v%,%,$(VERSION))
DEBBUILD_VERN := $(firstword $(subst -, ,$(DEBBUILD_VERSION)))
DEBBUILD_DATE := $(shell git log -1 --format="%cd" --date=rfc 2>/dev/null)
DEBBUILD_PACKAGE := telego_$(DEBBUILD_VERN)_amd64.deb

.PHONY: debbuild_make_workflow debbuild_copy_files debbuild_environment_set deb

debbuild_make_workflow:
	@mkdir -p "$(DEBBUILD_DIR)/debian" "$(DEBBUILD_DIR)/sources/etc/telego" \
		"$(DEBBUILD_DIR)/sources/usr/bin" "$(DEBBUILD_DIR)/sources/usr/share/doc/telego"
#		"$(DEBBUILD_DIR)/sources/etc/sysconfig" "$(DEBBUILD_DIR)/sources/etc/logrotate"

# Both prerequisites must finish before staging, including under make -j.
debbuild_copy_files: build debbuild_make_workflow
	cp -p "$(BINARY)" "$(DEBBUILD_DIR)/sources/usr/bin/telego"
	cp -p "$(MAKEFILE_DIR)config.example.toml" "$(DEBBUILD_DIR)/sources/etc/telego/telego.toml"

#	cp -p "$(DEBBUILD_DIST)/telego.service" "$(DEBBUILD_DIR)/debian/telego.service"
	sed -e 's|/usr/local/bin/telego|/usr/bin/$(BINARY)|g' \
		-e 's|CONFIG_PATH|$(CONFIG)|g' $(MAKEFILE_DIR)dist/telego.service > $(DEBBUILD_DIR)/debian/telego.service


#	cp -p "$(DEBBUILD_DIST)/rpmbuild.sysconfig" "$(DEBBUILD_DIR)/sources/etc/sysconfig/telego.sysconfig"
#	cp -p "$(DEBBUILD_DIST)/rpmbuild.logrotate" "$(DEBBUILD_DIR)/sources/etc/logrotate/telego.logrotate"

	cp -p "$(MAKEFILE_DIR)config.example.toml" "$(DEBBUILD_DIR)/sources/usr/share/doc/telego/"
	cp -p "$(MAKEFILE_DIR)LICENSE" "$(DEBBUILD_DIR)/sources/usr/share/doc/telego/"
	cp -p "$(MAKEFILE_DIR)README.md" "$(DEBBUILD_DIR)/sources/usr/share/doc/telego/"
	cp -p "$(MAKEFILE_DIR)README.ru.md" "$(DEBBUILD_DIR)/sources/usr/share/doc/telego/"
	cp -p "$(DEBBUILD_DIST)/debbuild.control" "$(DEBBUILD_DIR)/debian/control"
#	cp -p "$(DEBBUILD_DIST)/debbuild.changelog" "$(DEBBUILD_DIR)/debian/changelog"
	sed -e 's|__VERSION__|$(DEBBUILD_VERN)|g' -e 's|__DATE_R__|$(DEBBUILD_DATE)|g' $(DEBBUILD_DIST)/debbuild.changelog > $(DEBBUILD_DIR)/debian/changelog

	cp -p "$(DEBBUILD_DIST)/debbuild.copyright" "$(DEBBUILD_DIR)/debian/copyright"
	cp -p "$(DEBBUILD_DIST)/debbuild.rules" "$(DEBBUILD_DIR)/debian/rules"
	cp -r "$(MAKEFILE_DIR)docs/releases/." "$(DEBBUILD_DIR)/sources/usr/share/doc/telego/"

debbuild_environment_set:
	@printf 'DEB version: %s\n' "$(DEBBUILD_VERN)"

deb: debbuild_copy_files debbuild_environment_set
	cd $(DEBBUILD_DIR) && debuild -us -uc -tc -b
	rm $(MAKEFILE_DIR).build/*.build $(MAKEFILE_DIR).build/*.buildinfo $(MAKEFILE_DIR).build/*.changes
	mv "$(MAKEFILE_DIR).build/$(DEBBUILD_PACKAGE)" "$(MAKEFILE_DIR)"
	rm -rf .build
