An experiment of service graceful shutdown

Ultimate goal:
To have a deployment without disconnecting/re-connecting from load balancers

Idea:
Sharing the same socket listener in multiple processes/applications

Reference reading:
https://grisha.org/blog/2014/06/03/graceful-restart-in-golang/

V1 prototype:
implementation based on FB grace library, without any external dependencies (/server)
https://github.com/facebookarchive/grace

V2 prototype (the `launcher`):
A parent process (the launcher) works for creation and distribution of socket listeners within the instance. 

During deployment, the launcher creates and distributes the required socket listeners to a new child process (the new version of application), and peacefully terminates the old child process (old version of application)

