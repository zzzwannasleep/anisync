# Project-level configuration.
#
# NOTE: the project() name below is an INTERNAL CMake identifier and does NOT
# decide the executable name. Flutter's tooling reads only BINARY_NAME -- see
# getCmakeExecutableName() in packages/flutter_tools/lib/src/cmake.dart, whose
# pattern is `^\s*set\(BINARY_NAME\s*"(.*)"\s*\)\s*$` and which never looks at
# project(). Nothing in this repository consumes CMAKE_PROJECT_NAME either.
# Left deliberately unchanged: renaming it would churn add_subdirectory
# scoping for zero user-visible effect.
cmake_minimum_required(VERSION 3.14)
project(anisync_app LANGUAGES CXX)

# The name of the executable created for the application. Change this to change
# the on-disk name of your application.
#
# This variable is the ONLY thing that decides the product name. It has to stay
# in sync with two places that cannot follow it automatically:
#   * runner/Runner.rc -- the FileDescription / InternalName / OriginalFilename
#     / ProductName string literals (static text, not CMake variables)
#   * README.md / docs/build.md -- the advertised artifact name (AniSync.exe)
# Everything inside the build that derives a target name, an install
# destination or an output directory uses the variable
# (add_executable(${BINARY_NAME}) / install(TARGETS ${BINARY_NAME}) /
# $<TARGET_FILE_DIR:${BINARY_NAME}>), so renaming here propagates by itself.
# Never hard-code the product name in a path.
set(BINARY_NAME "AniSync")

# Explicitly opt in to modern CMake behaviors to avoid warnings with recent
# versions of CMake.
cmake_policy(VERSION 3.14...3.25)

# Define build configuration option.
get_property(IS_MULTICONFIG GLOBAL PROPERTY GENERATOR_IS_MULTI_CONFIG)
if(IS_MULTICONFIG)
  set(CMAKE_CONFIGURATION_TYPES "Debug;Profile;Release"
    CACHE STRING "" FORCE)
else()
  if(NOT CMAKE_BUILD_TYPE AND NOT CMAKE_CONFIGURATION_TYPES)
    set(CMAKE_BUILD_TYPE "Debug" CACHE
      STRING "Flutter build mode" FORCE)
    set_property(CACHE CMAKE_BUILD_TYPE PROPERTY STRINGS
      "Debug" "Profile" "Release")
  endif()
endif()
# Define settings for the Profile build mode.
set(CMAKE_EXE_LINKER_FLAGS_PROFILE "${CMAKE_EXE_LINKER_FLAGS_RELEASE}")
set(CMAKE_SHARED_LINKER_FLAGS_PROFILE "${CMAKE_SHARED_LINKER_FLAGS_RELEASE}")
set(CMAKE_C_FLAGS_PROFILE "${CMAKE_C_FLAGS_RELEASE}")
set(CMAKE_CXX_FLAGS_PROFILE "${CMAKE_CXX_FLAGS_RELEASE}")

# Use Unicode for all projects.
add_definitions(-DUNICODE -D_UNICODE)

# Compilation settings that should be applied to most targets.
#
# Be cautious about adding new options here, as plugins use this function by
# default. In most cases, you should add new options to specific targets instead
# of modifying this function.
function(APPLY_STANDARD_SETTINGS TARGET)
  target_compile_features(${TARGET} PUBLIC cxx_std_17)
  target_compile_options(${TARGET} PRIVATE /W4 /WX /wd"4100")
  target_compile_options(${TARGET} PRIVATE /EHsc)
  target_compile_definitions(${TARGET} PRIVATE "_HAS_EXCEPTIONS=0")
  target_compile_definitions(${TARGET} PRIVATE "$<$<CONFIG:Debug>:_DEBUG>")
endfunction()

# Flutter library and tool build rules.
set(FLUTTER_MANAGED_DIR "${CMAKE_CURRENT_SOURCE_DIR}/flutter")
add_subdirectory(${FLUTTER_MANAGED_DIR})

# Application build; see runner/CMakeLists.txt.
add_subdirectory("runner")


# Generated plugin build rules, which manage building the plugins and adding
# them to the application.
include(flutter/generated_plugins.cmake)


# === AniSync core (c-shared Go library) ===
#
# The Go engine is compiled by scripts/build-core.ps1 into app/build/native/
# (see docs/build.md). Dart loads anisync_core.dll from the directory the GUI
# executable lives in -- docs/ops-api.md §1.2 pins that location, and
# ANISYNC_CORE_LIB can override it for tests only.
#
# The DLL therefore has to be copied into the bundle root in BOTH places below:
#   * POST_BUILD  -> the build tree, so `flutter run -d windows` finds it
#   * install()   -> the install tree, so the packaged release finds it
# Shipping only one of the two is the classic "runs fine locally, library not
# found after install" bug, which is why both are spelled out here.
if(CMAKE_SYSTEM_PROCESSOR MATCHES "^(ARM64|arm64|aarch64)$")
  set(ANISYNC_CORE_ARCH "arm64")
else()
  set(ANISYNC_CORE_ARCH "amd64")
endif()
set(ANISYNC_CORE_DLL
  "${CMAKE_CURRENT_SOURCE_DIR}/../build/native/windows-${ANISYNC_CORE_ARCH}/anisync_core.dll"
  CACHE FILEPATH "AniSync c-shared core DLL (built by scripts/build-core.ps1)")

if(EXISTS "${ANISYNC_CORE_DLL}")
  set(ANISYNC_HAVE_CORE TRUE)
else()
  set(ANISYNC_HAVE_CORE FALSE)
  message(STATUS
    "AniSync core not found at ${ANISYNC_CORE_DLL} -- the runner will build, "
    "but it cannot load the engine at runtime. Build it with "
    "scripts/build-core.ps1, or pass -DANISYNC_CORE_DLL=<abs path>.")
endif()

# Release/CI builds must not silently produce an app with no engine.
# Default OFF so plain UI work does not need the Go toolchain; app.yml turns
# it ON for the jobs that ship a bundle.
option(ANISYNC_REQUIRE_CORE
  "Fail configuration when anisync_core.dll is missing" OFF)
if(ANISYNC_REQUIRE_CORE AND NOT ANISYNC_HAVE_CORE)
  message(FATAL_ERROR
    "ANISYNC_REQUIRE_CORE=ON but anisync_core.dll was not found at "
    "${ANISYNC_CORE_DLL}. Run scripts/build-core.ps1 -OS windows "
    "-Arch ${ANISYNC_CORE_ARCH} first (docs/build.md).")
endif()

if(ANISYNC_HAVE_CORE)
  add_custom_command(TARGET ${BINARY_NAME} POST_BUILD
    COMMAND ${CMAKE_COMMAND} -E copy_if_different
      "${ANISYNC_CORE_DLL}" "$<TARGET_FILE_DIR:${BINARY_NAME}>"
    COMMENT "Copying anisync_core.dll next to the runner executable")
endif()


# === Installation ===
# Support files are copied into place next to the executable, so that it can
# run in place. This is done instead of making a separate bundle (as on Linux)
# so that building and running from within Visual Studio will work.
set(BUILD_BUNDLE_DIR "$<TARGET_FILE_DIR:${BINARY_NAME}>")
# Make the "install" step default, as it's required to run.
set(CMAKE_VS_INCLUDE_INSTALL_TO_DEFAULT_BUILD 1)
if(CMAKE_INSTALL_PREFIX_INITIALIZED_TO_DEFAULT)
  set(CMAKE_INSTALL_PREFIX "${BUILD_BUNDLE_DIR}" CACHE PATH "..." FORCE)
endif()

set(INSTALL_BUNDLE_DATA_DIR "${CMAKE_INSTALL_PREFIX}/data")
set(INSTALL_BUNDLE_LIB_DIR "${CMAKE_INSTALL_PREFIX}")

install(TARGETS ${BINARY_NAME} RUNTIME DESTINATION "${CMAKE_INSTALL_PREFIX}"
  COMPONENT Runtime)

install(FILES "${FLUTTER_ICU_DATA_FILE}" DESTINATION "${INSTALL_BUNDLE_DATA_DIR}"
  COMPONENT Runtime)

install(FILES "${FLUTTER_LIBRARY}" DESTINATION "${INSTALL_BUNDLE_LIB_DIR}"
  COMPONENT Runtime)

# The install-tree half of the pair above: without this the bundle built by
# `cmake --install` / the packaging step has no engine next to the exe.
if(ANISYNC_HAVE_CORE)
  install(FILES "${ANISYNC_CORE_DLL}" DESTINATION "${INSTALL_BUNDLE_LIB_DIR}"
    COMPONENT Runtime)
endif()

if(PLUGIN_BUNDLED_LIBRARIES)
  install(FILES "${PLUGIN_BUNDLED_LIBRARIES}"
    DESTINATION "${INSTALL_BUNDLE_LIB_DIR}"
    COMPONENT Runtime)
endif()

# Copy the native assets provided by the build.dart from all packages.
set(NATIVE_ASSETS_DIR "${PROJECT_BUILD_DIR}native_assets/windows/")
install(DIRECTORY "${NATIVE_ASSETS_DIR}"
   DESTINATION "${INSTALL_BUNDLE_LIB_DIR}"
   COMPONENT Runtime)

# Fully re-copy the assets directory on each build to avoid having stale files
# from a previous install.
set(FLUTTER_ASSET_DIR_NAME "flutter_assets")
install(CODE "
  file(REMOVE_RECURSE \"${INSTALL_BUNDLE_DATA_DIR}/${FLUTTER_ASSET_DIR_NAME}\")
  " COMPONENT Runtime)
install(DIRECTORY "${PROJECT_BUILD_DIR}/${FLUTTER_ASSET_DIR_NAME}"
  DESTINATION "${INSTALL_BUNDLE_DATA_DIR}" COMPONENT Runtime)

# Install the AOT library on non-Debug builds only.
install(FILES "${AOT_LIBRARY}" DESTINATION "${INSTALL_BUNDLE_DATA_DIR}"
  CONFIGURATIONS Profile;Release
  COMPONENT Runtime)
