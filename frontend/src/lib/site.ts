export const repositoryURL = "https://github.com/biubiuqiu/lester-agent";
export const issuesURL = `${repositoryURL}/issues`;
export const cloneCommand = `git clone ${repositoryURL}.git\ncd lester-agent`;
export const startCommand = "docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build";
export const setupCommands = `${cloneCommand}\ncp deploy/.env.example deploy/.env\n# 按部署指南填写密钥后启动\n${startCommand}`;
