cmake_minimum_required(VERSION 3.16)
project(aps_executor_c C)

add_library(aps_executor_c SHARED src/plugin.c)
set_target_properties(aps_executor_c PROPERTIES
    OUTPUT_NAME "executor-c"
    PREFIX ""
)
