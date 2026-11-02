# LinaPro 插件 Makefile。
# 共享的代码生成目标位于仓库根目录。

PLUGIN_ROOT := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
REPO_ROOT := $(abspath $(PLUGIN_ROOT)/../../..)

include $(REPO_ROOT)/hack/makefiles/plugin.codegen.mk
