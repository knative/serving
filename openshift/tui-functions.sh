#!/usr/bin/env bash

# Colors
COLOR_OFF='\033[0m' # Text Reset
RED='\033[0;31m'    # Red
GREEN='\033[0;32m'  # Green
YELLOW='\033[0;93m' # Yellow
COLOR=""            # Points to current set color

# Use the following functions to help printing your script output in a more user-friendly and readable
# format. If you group your script's logic into a set of logical stages, each executing a set of steps,
# then use the 'stage' function to print the main stage message, then the 'step' for each step message
# and step_error for each step error message.

# Prints its argument within a box.
# It supports a maximum message of 120 characters.
# Will print in 60 ch box if the message fits.
function text_box() {
  MSG=$*
  MSG_SIZE=${#MSG}
  [[ $MSG_SIZE -lt 60 ]] && WIDTH=60 || WIDTH=120
  printf "┌"
  printf "─%.0s" $(seq 1 $WIDTH)
  printf "┐"
  printf "\n│ "
  ((PADDING = ($WIDTH - $MSG_SIZE) - 1))
  printf "${COLOR}${MSG}${COLOR_OFF}"
  printf " %.0s" $(seq 1 $PADDING)
  printf "│\n"
  printf "└"
  printf "─%.0s" $(seq 1 $WIDTH)
  printf "┘"
  printf "\n"
}

# Prints its argument indented as an indented text with a leeding bread crump indicator.
function box_sub_text() {
  MSG=$*
  printf "│─── "
  printf "${COLOR}${MSG}${COLOR_OFF}"
  printf "\n"
}

# Prints a stage header message of max 120 ch in green.
function stage() {
  COLOR=$GREEN
  text_box "${*}..."
}

# Prints a normal step message in green.
function step() {
  COLOR=$GREEN
  box_sub_text $*
}

# Prints an error step message in red.
function step_error() {
  COLOR=$RED
  box_sub_text $*
}

# Prints a warning step message in yellow.
function step_warn() {
  MSG=$*
  printf "${YELLOW}│─── "
  printf "⚠️ ${MSG}${COLOR_OFF} ⚠️"
  printf "\n"
}

# Prints a stage warning header message of max 120 ch in yellow.
function stage_warn() {
  MSG=$*
  MSG_SIZE=${#MSG}
  [[ $MSG_SIZE -lt 60 ]] && WIDTH=60 || WIDTH=120
  printf "${YELLOW}┌"
  printf "─%.0s" $(seq 1 $WIDTH)
  printf "┐"
  printf "\n│ ${COLOR_OFF}"
  ((PADDING = ($WIDTH - $MSG_SIZE) - 1))
  printf "${MSG}"
  printf " %.0s" $(seq 1 $PADDING)
  printf "${YELLOW}│\n"
  printf "└"
  printf "─%.0s" $(seq 1 $WIDTH)
  printf "┘${COLOR_OFF}"
  printf "\n"
}
