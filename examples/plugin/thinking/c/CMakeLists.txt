cmake_minimum_required(VERSION 3.16)
project(aps_thinking_c C)

add_library(aps_thinking_c SHARED src/plugin.c)
set_target_properties(aps_thinking_c PROPERTIES
    OUTPUT_NAME "thinking-c"
    PREFIX ""
)
