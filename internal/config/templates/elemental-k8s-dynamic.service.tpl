[Unit]
Description=Elemental K8s Dynamic Configuration
After=network-online.target
Before=k8s-config-installer.service
Wants=network-online.target
ConditionPathExists=!/run/elemental/k8s-dynamic-applied

[Service]
Type=oneshot
RemainAfterExit=yes
TimeoutSec={{ .Timeout }}
ExecStartPre=/bin/mkdir -p /run/elemental
ExecStart=/usr/bin/elemental3ctl k8s-dynamic apply
ExecStartPost=/bin/touch /run/elemental/k8s-dynamic-applied

[Install]
WantedBy=multi-user.target
