#!/usr/bin/perl
# Answers GET /v1/api-key so Grok will start a turn.
# Every other call is forwarded to the gate, which adds the key.
use strict;
use warnings;
use IO::Socket::INET;

my $addr = $ENV{SHIM_ADDR} // "127.0.0.1";
my $port = $ENV{SHIM_PORT} // 18099;
my $up_host = $ENV{SHIM_UPSTREAM_HOST} // "grok.hermetarium.test";
my $up_port = $ENV{SHIM_UPSTREAM_PORT} // 80;
my $max_body = $ENV{SHIM_MAX_BODY} // 64 * 1024 * 1024;

$SIG{CHLD} = "IGNORE";

my $srv = IO::Socket::INET->new(
    LocalAddr => $addr,
    LocalPort => $port,
    Listen    => 32,
    Reuse     => 1,
    Proto     => "tcp",
) or die "listen $addr:$port: $!\n";

while (my $client = $srv->accept) {
    defined(my $pid = fork()) or die "fork: $!\n";
    if ($pid) {
        close $client;
        next;
    }
    close $srv;
    eval { handle($client) };
    close $client;
    exit 0;
}

sub handle {
    my ($client) = @_;
    binmode $client;
    my ($head, $rest) = read_headers($client);
    return unless defined $head;
    my ($method, $target) = $head =~ m{\A([A-Z]+) (\S+) HTTP/\d\.\d};
    return unless defined $method;
    my ($path) = split /\?/, $target, 2;

    if ($path =~ m{\A/v1/api-key/?\z}) {
        read_body($client, $head, $rest);
        my $body = '{"status":"ok"}';
        syswrite_all(
            $client,
            "HTTP/1.1 200 OK\r\n"
                . "Content-Type: application/json\r\n"
                . "Content-Length: " . length($body) . "\r\n"
                . "Connection: close\r\n\r\n"
                . $body
        );
        return;
    }

    my $body = read_body($client, $head, $rest);
    unless (defined $body) {
        my $msg = "bad request\n";
        syswrite_all(
            $client,
            "HTTP/1.1 400 Bad Request\r\nContent-Type: text/plain\r\n"
                . "Content-Length: " . length($msg) . "\r\nConnection: close\r\n\r\n"
                . $msg
        );
        return;
    }
    my $up = IO::Socket::INET->new(
        PeerHost => $up_host,
        PeerPort => $up_port,
        Proto    => "tcp",
    );
    unless ($up) {
        my $msg = "upstream unavailable\n";
        syswrite_all(
            $client,
            "HTTP/1.1 502 Bad Gateway\r\nContent-Type: text/plain\r\n"
                . "Content-Length: " . length($msg) . "\r\nConnection: close\r\n\r\n"
                . $msg
        );
        return;
    }
    binmode $up;
    syswrite_all($up, rewrite_request($head, $target, $body));
    syswrite_all($up, $body) if length $body;
    pump($client, $up);
    close $up;
}

sub read_headers {
    my ($sock) = @_;
    my $buf = "";
    while ($buf !~ /\r\n\r\n/) {
        my $n = sysread($sock, my $chunk, 8192);
        return unless $n;
        $buf .= $chunk;
        return if length($buf) > 1024 * 1024;
    }
    my $at = index($buf, "\r\n\r\n");
    return (substr($buf, 0, $at), substr($buf, $at + 4));
}

sub read_body {
    my ($sock, $head, $rest) = @_;
    if ($head =~ /Transfer-Encoding:\s*chunked/i) {
        return read_chunked($sock, $rest);
    }
    my ($len) = $head =~ /Content-Length:\s*(\d+)/i;
    $len //= 0;
    return "" if $len == 0;
    return if $len > $max_body;
    my $buf = $rest;
    while (length($buf) < $len) {
        my $want = $len - length($buf);
        $want = 65536 if $want > 65536;
        my $n = sysread($sock, my $chunk, $want);
        last unless $n;
        $buf .= $chunk;
    }
    return if length($buf) < $len;
    return substr($buf, 0, $len);
}

sub read_chunked {
    my ($sock, $buf) = @_;
    my $out = "";
    while (1) {
        while ($buf !~ /\r\n/) {
            my $n = sysread($sock, my $chunk, 8192);
            return unless $n;
            $buf .= $chunk;
        }
        my ($line, $rest) = split /\r\n/, $buf, 2;
        my ($hex) = $line =~ /^([0-9A-Fa-f]+)/;
        return unless defined $hex;
        my $size = hex($hex);
        return if length($out) + $size > $max_body;
        $buf = $rest;
        my $need = $size + 2;
        while (length($buf) < $need) {
            my $n = sysread($sock, my $chunk, 8192);
            return unless $n;
            $buf .= $chunk;
        }
        $out .= substr($buf, 0, $size);
        $buf = substr($buf, $need);
        last if $size == 0;
    }
    return $out;
}

sub rewrite_request {
    my ($head, $target, $body) = @_;
    my ($start) = $head =~ m{\A([^\r\n]+)};
    my @out;
    my $rest = $head;
    $rest =~ s/\A[^\r\n]+\r?\n//;
    for my $line (split /\r\n/, $rest) {
        next if $line eq "";
        next if $line =~ /^(?:Host|Connection|Proxy-Connection|Keep-Alive|Transfer-Encoding|Content-Length)\s*:/i;
        push @out, $line;
    }
    push @out, "Host: $up_host";
    push @out, "Content-Length: " . length($body);
    push @out, "Connection: close";
    return "$start\r\n" . join("\r\n", @out) . "\r\n\r\n";
}

sub pump {
    my ($dst, $src) = @_;
    while (1) {
        my $n = sysread($src, my $buf, 65536);
        last unless $n;
        syswrite_all($dst, $buf);
    }
}

sub syswrite_all {
    my ($sock, $buf) = @_;
    my $off = 0;
    while ($off < length($buf)) {
        my $n = syswrite($sock, $buf, length($buf) - $off, $off);
        return unless $n;
        $off += $n;
    }
}
