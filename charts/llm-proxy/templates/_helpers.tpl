{{- define "llm-proxy.configureEnv" -}}
{{- $env := list -}}

{{- $listenAddress := trimAll " \n\t" (default ":8080" .Values.llmProxy.listenAddress) -}}
{{- if $listenAddress }}
{{- $env = append $env (dict "name" "LISTEN_ADDRESS" "value" $listenAddress) -}}
{{- end }}

{{- $llmAddress := trimAll " \n\t" (default "llm:50051" .Values.llmProxy.llmServiceAddress) -}}
{{- if $llmAddress }}
{{- $env = append $env (dict "name" "LLM_SERVICE_ADDRESS" "value" $llmAddress) -}}
{{- end }}

{{- $usersAddress := trimAll " \n\t" (default "users:50051" .Values.llmProxy.usersServiceAddress) -}}
{{- if $usersAddress }}
{{- $env = append $env (dict "name" "USERS_SERVICE_ADDRESS" "value" $usersAddress) -}}
{{- end }}

{{- $agentsAddress := trimAll " \n\t" (default "agents:50051" .Values.llmProxy.agentsServiceAddress) -}}
{{- if $agentsAddress }}
{{- $env = append $env (dict "name" "AGENTS_SERVICE_ADDRESS" "value" $agentsAddress) -}}
{{- end }}

{{- $authzAddress := trimAll " \n\t" (default "authorization:50051" .Values.llmProxy.authorizationServiceAddress) -}}
{{- if $authzAddress }}
{{- $env = append $env (dict "name" "AUTHORIZATION_SERVICE_ADDRESS" "value" $authzAddress) -}}
{{- end }}

{{- $notificationsAddress := trimAll " \n\t" (default "notifications:50051" .Values.llmProxy.notificationsServiceAddress) -}}
{{- if $notificationsAddress }}
{{- $env = append $env (dict "name" "NOTIFICATIONS_SERVICE_ADDRESS" "value" $notificationsAddress) -}}
{{- end }}

{{- $egressCACert := trimAll " \n\t" (default "/var/run/agyn/egress-ca/tls.crt" .Values.llmProxy.egressCACertPath) -}}
{{- if $egressCACert }}
{{- $env = append $env (dict "name" "EGRESS_CA_CERT_PATH" "value" $egressCACert) -}}
{{- end }}
{{- $egressCAKey := trimAll " \n\t" (default "/var/run/agyn/egress-ca/tls.key" .Values.llmProxy.egressCAKeyPath) -}}
{{- if $egressCAKey }}
{{- $env = append $env (dict "name" "EGRESS_CA_KEY_PATH" "value" $egressCAKey) -}}
{{- end }}

{{- $zitiMgmtAddress := trimAll " \n\t" (default "ziti-management:50051" .Values.llmProxy.zitiManagementAddress) -}}
{{- if $zitiMgmtAddress }}
{{- $env = append $env (dict "name" "ZITI_MANAGEMENT_ADDRESS" "value" $zitiMgmtAddress) -}}
{{- end }}

{{- $zitiEnabled := default false .Values.llmProxy.zitiEnabled -}}
{{- $env = append $env (dict "name" "ZITI_ENABLED" "value" (printf "%t" $zitiEnabled)) -}}

{{- $zitiLease := trimAll " \n\t" (default "2m" .Values.llmProxy.zitiLeaseRenewalInterval) -}}
{{- if $zitiLease }}
{{- $env = append $env (dict "name" "ZITI_LEASE_RENEWAL_INTERVAL" "value" $zitiLease) -}}
{{- end }}

{{- $zitiEnrollmentTimeout := trimAll " \n\t" (default "5m" .Values.llmProxy.zitiEnrollmentTimeout) -}}
{{- if $zitiEnrollmentTimeout }}
{{- $env = append $env (dict "name" "ZITI_ENROLLMENT_TIMEOUT" "value" $zitiEnrollmentTimeout) -}}
{{- end }}

{{- $userEnv := .Values.env | default (list) -}}
{{- $_ := set .Values "env" (concat $env $userEnv) -}}
{{- end -}}
