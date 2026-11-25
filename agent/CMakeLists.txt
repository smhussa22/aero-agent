cmake_minimum_required(VERSION 3.22)
project(aero_agent LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# AERO_STATIC=ON builds one fully static binary against a gRPC compiled from source (deploy/docker/Dockerfile.agent);
# the default uses the system's gRPC through pkg-config
option(AERO_STATIC "fully static binary against a source-built gRPC" OFF)
if(AERO_STATIC)
    # gRPC's config only resolves once its bundled abseil and protobuf have been found by their own configs
    find_package(absl CONFIG REQUIRED)
    find_package(protobuf CONFIG REQUIRED)
    find_package(gRPC CONFIG REQUIRED)
    set(Protobuf_PROTOC_EXECUTABLE $<TARGET_FILE:protobuf::protoc>)
    set(GRPC_CPP_PLUGIN $<TARGET_FILE:gRPC::grpc_cpp_plugin>)
    set(AERO_GRPC_LIBS gRPC::grpc++ protobuf::libprotobuf)
    add_link_options(-static)
else()
    find_package(Protobuf REQUIRED)
    find_package(PkgConfig REQUIRED)
    pkg_check_modules(GRPCPP REQUIRED IMPORTED_TARGET grpc++)
    find_program(GRPC_CPP_PLUGIN grpc_cpp_plugin REQUIRED)
    set(AERO_GRPC_LIBS protobuf::libprotobuf PkgConfig::GRPCPP)
endif()

# generate C++ from the shared proto
set(PROTO_DIR ${CMAKE_CURRENT_SOURCE_DIR}/../proto)
set(GEN_DIR ${CMAKE_CURRENT_BINARY_DIR}/gen)
file(MAKE_DIRECTORY ${GEN_DIR})
set(PROTO_SRCS ${GEN_DIR}/metrics.pb.cc ${GEN_DIR}/metrics.grpc.pb.cc)

add_custom_command(
    OUTPUT ${PROTO_SRCS} ${GEN_DIR}/metrics.pb.h ${GEN_DIR}/metrics.grpc.pb.h
    COMMAND ${Protobuf_PROTOC_EXECUTABLE}
            --proto_path=${PROTO_DIR}
            --cpp_out=${GEN_DIR}
            --grpc_out=${GEN_DIR}
            --plugin=protoc-gen-grpc=${GRPC_CPP_PLUGIN}
            ${PROTO_DIR}/metrics.proto
    DEPENDS ${PROTO_DIR}/metrics.proto
)

add_library(aero_proto STATIC ${PROTO_SRCS})
target_include_directories(aero_proto PUBLIC ${GEN_DIR})
target_link_libraries(aero_proto PUBLIC ${AERO_GRPC_LIBS})

add_library(aero_collectors STATIC
    src/CpuCollector.cc
    src/MemoryCollector.cc
    src/DiskCollector.cc
    src/NetworkCollector.cc
    src/ProcessCollector.cc
    src/MetricsClient.cc
)
target_include_directories(aero_collectors PUBLIC include)
target_link_libraries(aero_collectors PUBLIC aero_proto)
target_compile_options(aero_collectors PRIVATE -Wall -Wextra)

add_executable(aero-agent src/main.cc)
target_link_libraries(aero-agent PRIVATE aero_collectors)

enable_testing()
add_executable(aero-agent-tests tests/test_collectors.cc)
target_link_libraries(aero-agent-tests PRIVATE aero_collectors)
add_test(NAME collectors COMMAND aero-agent-tests)
