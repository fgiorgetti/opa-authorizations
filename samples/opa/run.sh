base_path=$(dirname $0)
opa run --server ${base_path}/../../policies
