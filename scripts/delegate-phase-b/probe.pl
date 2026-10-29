#!/usr/bin/env perl
# Probes for scripts/delegate-phase-b.sh. B7: flock exclusivity, O_APPEND
# line integrity and rename atomicity, run from either side of a
# host-container bind mount. D8: SGR styling in a `herdr pane read --format
# ansi` capture. Core perl only (Fcntl), so it runs on macOS's system perl and
# in the devcontainer.
#
#   probe.pl hold FILE SECS       take LOCK_EX, write FILE.held, keep the lock
#                                 until FILE.release appears or SECS pass
#   probe.pl try FILE             LOCK_EX|LOCK_NB once: prints got or busy
#   probe.pl append FILE TAG P N  P processes each append N 64-byte lines
#   probe.pl count FILE EXPECTED  prints lines, good, bad and expected
#   probe.pl renamer FILE SECS    rewrite FILE by temp file plus rename
#   probe.pl reader FILE SECS     read FILE in a loop: reads, torn, missing
#   probe.pl dim FILE [MARKER]    count characters drawn dim or grey; with
#                                 MARKER, only on lines that start with it
#   probe.pl style FILE TEXT      the style TEXT is drawn in: dim, grey,
#                                 default, other, or absent
#
# append, renamer and reader take an optional last GOFILE argument: they wait
# for it to exist so both sides of the mount start together.
use strict;
use warnings;
use Fcntl qw(:flock O_WRONLY O_APPEND O_CREAT);

my $SIZE = 4096;
my $cmd  = shift @ARGV or die "usage: probe.pl hold|try|append|count|renamer|reader ...\n";

sub nap { select(undef, undef, undef, $_[0]) }

# go GOFILE: wait (up to 60 s) for the start signal, if one was given.
sub go {
    my ($g) = @_;
    return unless defined $g;
    my $end = time + 60;
    nap(0.01) while !-e $g && time < $end;
}

sub hold {
    my ($f, $secs) = @_;
    open(my $fh, '>>', $f) or die "open $f: $!\n";
    flock($fh, LOCK_EX) or die "flock $f: $!\n";
    open(my $m, '>', "$f.held") or die "open $f.held: $!\n";
    close $m;
    my $end = time + $secs;
    nap(0.2) while time < $end && !-e "$f.release";
    unlink "$f.held", "$f.release";
    print "held\n";
}

sub try_lock {
    my ($f) = @_;
    open(my $fh, '>>', $f) or die "open $f: $!\n";
    print flock($fh, LOCK_EX | LOCK_NB) ? "got\n" : "busy\n";
}

sub append {
    my ($f, $tag, $procs, $n, $g) = @_;
    go($g);
    my @kids;
    for my $p (1 .. $procs) {
        my $pid = fork();
        die "fork: $!\n" unless defined $pid;
        if ($pid == 0) {
            sysopen(my $fh, $f, O_WRONLY | O_APPEND | O_CREAT) or die "open $f: $!\n";
            for my $i (1 .. $n) {
                my $head = sprintf('%s %d %d ', $tag, $p, $i);
                syswrite($fh, $head . ('x' x (63 - length $head)) . "\n");
            }
            exit 0;
        }
        push @kids, $pid;
    }
    waitpid($_, 0) for @kids;
    print "appended " . ($procs * $n) . "\n";
}

sub count {
    my ($f, $expected) = @_;
    open(my $fh, '<', $f) or die "open $f: $!\n";
    my ($lines, $good, %by) = (0, 0);
    while (my $l = <$fh>) {
        $lines++;
        next unless length($l) == 64 && $l =~ /^(\S+) \d+ \d+ x+\n\z/;
        $good++;
        $by{$1}++;
    }
    my $per = join ' ', map { "$_=$by{$_}" } sort keys %by;
    printf "lines=%d good=%d bad=%d expected=%d (by writer: %s)\n", $lines, $good, $lines - $good, $expected, $per || 'none';
}

sub renamer {
    my ($f, $secs, $g) = @_;
    go($g);
    my ($k, $end) = (0, time + $secs);
    while (time < $end) {
        my $c   = $k % 2 ? 'B' : 'A';
        my $tmp = "$f.tmp.$$";
        open(my $fh, '>', $tmp) or die "open $tmp: $!\n";
        print $fh $c x $SIZE;
        close $fh or die "close $tmp: $!\n";
        rename($tmp, $f) or die "rename $tmp: $!\n";
        $k++;
    }
    print "renames=$k\n";
}

sub reader {
    my ($f, $secs, $g) = @_;
    go($g);
    my ($reads, $torn, $missing, $end) = (0, 0, 0, time + $secs);
    while (time < $end) {
        my $fh;
        unless (open($fh, '<', $f)) { $missing++; next; }
        local $/;
        my $s = <$fh>;
        close $fh;
        $reads++;
        $torn++ unless defined $s && length($s) == $SIZE && $s =~ /\A(?:A+|B+)\z/;
    }
    print "reads=$reads torn=$torn missing=$missing\n";
}

# runs FILE: the text split into [style, text] runs, tracking SGR dim (2 / 22)
# and the foreground colour (30-37, 90-97, 38;5;N, 38;2;R;G;B, 39, 0).
sub runs {
    my ($f) = @_;
    open(my $fh, '<', $f) or die "open $f: $!\n";
    local $/;
    my $s = <$fh> // '';
    my ($dim, $fg, @out) = (0, '');
    for my $part (split /(\e\[[0-9;]*m)/, $s) {
        if ($part =~ /^\e\[([0-9;]*)m$/) {
            my @c = length $1 ? split(/;/, $1) : (0);
            while (@c) {
                my $c = shift @c;
                if    ($c eq '' || $c == 0) { ($dim, $fg) = (0, '') }
                elsif ($c == 2)  { $dim = 1 }
                elsif ($c == 22) { $dim = 0 }
                elsif ($c == 39) { $fg = '' }
                elsif (($c >= 30 && $c <= 37) || ($c >= 90 && $c <= 97)) { $fg = $c }
                elsif ($c == 38 && @c && $c[0] == 5) { shift @c; $fg = '5;' . (shift(@c) // '') }
                elsif ($c == 38 && @c && $c[0] == 2) { shift @c; $fg = '2;' . join(';', splice(@c, 0, 3)) }
                # Background (48) and underline (58) colours: skip their
                # arguments, or the 2 of 48;2;R;G;B reads as dim (bug-ec114e74).
                elsif (($c == 48 || $c == 58) && @c && $c[0] == 5) { splice(@c, 0, 2) }
                elsif (($c == 48 || $c == 58) && @c && $c[0] == 2) { splice(@c, 0, 4) }
            }
            next;
        }
        $part =~ s/\e\[[0-9;?]*[ -\/]*[@-~]//g;
        push @out, [style($dim, $fg), $part] if length $part;
    }
    return @out;
}

sub style {
    my ($dim, $fg) = @_;
    return 'dim' if $dim;
    return 'default' if $fg eq '';
    return 'grey' if $fg eq '90' || $fg eq '5;8';
    if ($fg =~ /^5;(\d+)$/) { return $1 >= 232 && $1 <= 250 ? 'grey' : 'other' }
    if ($fg =~ /^2;(\d+);(\d+);(\d+)$/) {
        my ($r, $g, $b) = ($1, $2, $3);
        return abs($r - $g) < 16 && abs($g - $b) < 16 && $r >= 64 && $r <= 200 ? 'grey' : 'other';
    }
    return 'other';
}

# dim FILE [MARKER]: with MARKER, count only on lines that start with it (the
# input box), so dim borders and status lines elsewhere do not count.
sub dim {
    my ($f, $marker) = @_;
    my ($n, $line, $count) = (0, '', 0);
    my $end = sub {
        $n += $count if !defined $marker || $line =~ /^\s*\Q$marker\E/;
        ($line, $count) = ('', 0);
    };
    for my $r (runs($f)) {
        my @bits = split /(\r?\n)/, $r->[1], -1;
        for my $b (@bits) {
            if ($b =~ /^\r?\n$/) { $end->(); next }
            $line .= $b;
            $count += () = $b =~ /\S/g if $r->[0] eq 'dim' || $r->[0] eq 'grey';
        }
    }
    $end->();
    print "dim_or_grey_chars=$n\n";
}

sub style_of {
    my ($f, $text) = @_;
    my $found = 'absent';
    for my $r (runs($f)) { $found = $r->[0] if index($r->[1], substr($text, 0, 6)) >= 0 }
    print "style=$found\n";
}

my %run = (hold => \&hold, try => \&try_lock, append => \&append, count => \&count,
           renamer => \&renamer, reader => \&reader, dim => \&dim, style => \&style_of);
my $fn = $run{$cmd} or die "unknown probe: $cmd\n";
$fn->(@ARGV);
