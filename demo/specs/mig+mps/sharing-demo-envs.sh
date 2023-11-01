GPU_TS_SHARING=$(nvidia-smi | awk '$3 == "N/A" && $4 == "N/A" && $6 == "C" && $7 ~ /sample/ {print $2}' | sort -u)
GPU_MPS_SHARING=$(nvidia-smi | awk '$3 == "N/A" && $4 == "N/A" && $6 == "M+C" && $7 ~ /sample/ {print $2}' | sort -u)
GI_TS_SHARING=$(nvidia-smi | awk '$3 != "N/A" && $4 != "N/A" && $6 == "C" && $7 ~ /sample/ {print $3}' | sort -u)
GI_MPS_SHARING=$(nvidia-smi | awk '$3 != "N/A" && $4 != "N/A" && $6 == "M+C" && $7 ~ /sample/ {print $3}' | sort -u)
MIG_TS_SHARING=$(nvidia-smi | awk -v gi="${GI_TS_SHARING}" '/MIG devices:/{p=1} /Processes:/{p=0} p && $3==gi {print $2 ":" $5}')
MIG_MPS_SHARING=$(nvidia-smi | awk -v gi="${GI_MPS_SHARING}" '/MIG devices:/{p=1} /Processes:/{p=0} p && $3==gi {print $2 ":" $5}')

echo export GPU_TS_SHARING_DEVICE=${GPU_TS_SHARING}
>&2 echo export GPU_TS_SHARING_DEVICE=${GPU_TS_SHARING}
echo export GPU_MPS_SHARING_DEVICE=${GPU_MPS_SHARING}
>&2 echo export GPU_MPS_SHARING_DEVICE=${GPU_MPS_SHARING}
echo export MIG_TS_SHARING_DEVICE=${MIG_TS_SHARING}
>&2 echo export MIG_TS_SHARING_DEVICE=${MIG_TS_SHARING}
echo export MIG_MPS_SHARING_DEVICE=${MIG_MPS_SHARING}
>&2 echo export MIG_MPS_SHARING_DEVICE=${MIG_MPS_SHARING}
