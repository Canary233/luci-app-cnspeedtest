include $(TOPDIR)/rules.mk
-include $(dir $(lastword $(MAKEFILE_LIST)))../version.mk

PKG_NAME:=luci-app-cnspeedtest
PKG_VERSION:=$(or $(CNSPEEDTEST_VERSION),0.1.0)
PKG_RELEASE:=1

include $(INCLUDE_DIR)/package.mk

define Package/luci-app-cnspeedtest
  SECTION:=luci
  CATEGORY:=LuCI
  SUBMENU:=3. Applications
  TITLE:=CnSpeedtest LuCI interface
  DEPENDS:=+cnspeedtest +luci-base +rpcd +jsonfilter
  PKGARCH:=all
endef

define Package/luci-app-cnspeedtest/description
LuCI web interface for CnSpeedtest.
endef

define Package/$(PKG_NAME)/postinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] || {
	chmod 0755 /usr/libexec/rpcd/cnspeedtest 2>/dev/null || true
	rm -rf /tmp/luci-* /tmp/luci-indexcache /tmp/luci-modulecache 2>/dev/null || true
	/etc/init.d/rpcd restart 2>/dev/null || true
}
exit 0
endef

define Package/$(PKG_NAME)/prerm
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] || {
	rm -rf /tmp/luci-* /tmp/luci-indexcache /tmp/luci-modulecache 2>/dev/null || true
}
exit 0
endef

define Build/Compile
endef

define Package/luci-app-cnspeedtest/install
	$(INSTALL_DIR) $(1)
	$(CP) ./root/. $(1)/
endef

$(eval $(call BuildPackage,luci-app-cnspeedtest))
