source develop.env

function cleanup() {
    rm -f backup-health-notifier
}
trap cleanup EXIT

GO111MODULE=on GOGC=off go build -mod=vendor -v -o backup-health-notifier .
./backup-health-notifier