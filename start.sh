# install dependencies
sudo apt update
sudo apt install git cmake scdoc

# build ydotool
# See: https://github.com/ReimuNotMoe/ydotool#build
git clone https://github.com/ReimuNotMoe/ydotool.git
cd ydotool
mkdir build
cd build
cmake ..                 # takes < 1 second
time make -j "$(nproc)"  # takes < 1 second
sudo make install
# Note: the install command installs here:
#
#       -- Install configuration: ""
#       -- Installing: /usr/local/bin/ydotoold
#       -- Installing: /usr/local/bin/ydotool
#       -- Installing: /usr/lib/systemd/user/ydotool.service
#       -- Installing: /usr/local/share/man/man1/ydotool.1
#       -- Installing: /usr/local/share/man/man8/ydotoold.8

# See the man pages for help, and to prove to yourself it is installed
man ydotool   # for the main program
man ydotoold  # for the background daemon process
# help menu
ydotool -h    # for the main program
ydotoold -h   # for the background daemon process [VERY HELPFUL MENU!]

# check the version; my output is `v1.0.4-0-g57ba7d0`
ydotoold --version
