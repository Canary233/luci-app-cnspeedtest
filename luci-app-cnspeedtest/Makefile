include $(TOPDIR)/rules.mk
-include $(dir $(lastword $(MAKEFILE_LIST)))../version.mk

PKG_NAME:=luci-app-cnspeedtest
PKG_VERSION:=$(or $(CNSPEEDTEST_VERSION),0.1.0)
PKG_RELEASE:=1

LUCI_TITLE:=CnSpeedtest LuCI interface
LUCI_DEPENDS:=+cnspeedtest +rpcd +jsonfilter
LUCI_PKGARCH:=all

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

include $(TOPDIR)/feeds/luci/luci.mk

# luci.mk handles the package build/install flow.
