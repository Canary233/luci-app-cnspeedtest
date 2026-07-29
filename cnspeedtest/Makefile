include $(TOPDIR)/rules.mk
-include $(dir $(lastword $(MAKEFILE_LIST)))../version.mk

PKG_NAME:=cnspeedtest
PKG_VERSION:=$(or $(CNSPEEDTEST_VERSION),0.1.0)
PKG_RELEASE:=1

PKG_BUILD_DIR:=$(BUILD_DIR)/$(PKG_NAME)-$(PKG_VERSION)
PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_USE_MIPS16:=0

GO_PKG:=cnspeedtest
GO_PKG_BUILD_PKG:=cnspeedtest
GO_PKG_LDFLAGS:=-s -w -buildid=

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

define Package/cnspeedtest
  SECTION:=net
  CATEGORY:=Network
  TITLE:=CnSpeedtest command line client
  URL:=https://dlcv2.cnspeedtest.cn/
  DEPENDS:=$(GO_ARCH_DEPENDS)
  HIDDEN:=1
endef

define Package/cnspeedtest/description
CnSpeedtest CLI for Linux/OpenWrt. It supports node discovery, manual node
selection, JSON/NDJSON output, interface binding on Linux, and download/upload
bandwidth sampling.
endef

define Build/Prepare
	mkdir -p $(PKG_BUILD_DIR)
	$(CP) ./src/. $(PKG_BUILD_DIR)/
endef

define Package/cnspeedtest/install
	$(call GoPackage/Package/Install/Bin,$(1))
	$(INSTALL_DIR) $(1)/bin
	ln -sf /usr/bin/cnspeedtest $(1)/bin/cnspeedtest
endef

$(eval $(call BuildPackage,cnspeedtest))
