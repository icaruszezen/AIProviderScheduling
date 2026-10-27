cmake_minimum_required(VERSION 3.16)
project(aps_usage_c C)

add_library(aps_usage_c SHARED src/plugin.c)
set_target_properties(aps_usage_c PROPERTIES
    OUTPUT_NAME "usage-c"
    PREFIX ""
)
