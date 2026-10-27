cmake_minimum_required(VERSION 3.16)
project(aps_response_translator_c C)

add_library(aps_response_translator_c SHARED src/plugin.c)
set_target_properties(aps_response_translator_c PROPERTIES
    OUTPUT_NAME "response-translator-c"
    PREFIX ""
)
