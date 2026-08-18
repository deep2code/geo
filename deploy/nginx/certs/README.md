# Nginx TLS 证书目录
#
# 将证书放入此目录后，在 nginx.conf 取消 443 server 块注释：
#   - fullchain.pem  证书链
#   - privkey.pem    私钥
#
# 获取证书：
#   - Let's Encrypt: certbot certonly --standalone -d your.domain
#   - 自签（测试）: openssl req -x509 -newkey rsa:2048 -keyout privkey.pem \
#       -out fullchain.pem -days 365 -nodes -subj "/CN=localhost"
