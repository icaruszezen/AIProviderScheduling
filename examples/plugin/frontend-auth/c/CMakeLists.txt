cmake_minimum_required(VERSION 3.16)
project(aps_frontend_auth_c C)

add_library(aps_frontend_auth_c SHARED src/plugin.c)
set_target_properties(aps_frontend_auth_c PROPERTIES
    OUTPUT_NAME "frontend-auth-c"
    PREFIX ""
)
