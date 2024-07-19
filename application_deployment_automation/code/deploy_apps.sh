#!/bin/bash

# Define the parent directory
parent_directory="../data/meta_func_deployment_100/apps"
curr_directory="../../../../code/"

# if [ "$(docker ps -q -f name=registry)" = "" ]; then
#     docker run -d -p 5000:5000 --name registry registry:latest
# fi

# Loop through subfolders
for subfolder in "$parent_directory"/*; do
    if [ -d "$subfolder" ]; then
        # echo "Entering $subfolder"
        cd "$subfolder" || exit 1  # Change to subfolder or exit if failed

        subfolder_name=$(basename "$subfolder")
        first_8_chars=$(echo "$subfolder_name" | cut -c 1-8)

        output_file="${subfolder_name}_output.txt"
        ls
        echo "$output_file"
    
        # Run your command with the extracted name and capture stdout
        docker build -t "application_${first_8_chars}.go" .
        docker image tag "application_${first_8_chars}.go" harbor.lincy.dev/femux/"application_${first_8_chars}.go"
        ⁠docker push harbor.lincy.dev/femux/"application_${first_8_chars}.go" > "$output_file" 2>&1
        
        # docker build -t "application_${first_8_chars}.go" .
        # docker image tag "application_${first_8_chars}.go" localhost:5000/"application_${first_8_chars}.go"
        # docker push localhost:5000/"application_${first_8_chars}.go" > "$output_file" 2>&1

        cd "$curr_directory"
    fi
done
