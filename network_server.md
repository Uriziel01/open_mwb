Technical Specification: AutoIt Network Control Server

Overview:

The server acts as a persistent TCP listener on 192.168.1.97:15102. The client should initiate a TCP connection, send a command string, receive an optional response (for retrieval commands), and close the connection.

Protocol Structure:

Transport: TCP.

Encoding: Plain text (string).

Format: Action:Parameter (Case-sensitive).

Delimiter: The first : acts as the separator. If no parameters are needed, just send the command string.

CCommand,Parameters,Description
pressKey,{KEY},Sends keystrokes to the persistent GUI input field using ControlSend.
moveMouse,"x,y","Moves the mouse cursor to absolute coordinates (x,y)."
setClipboard,string,Sets the system clipboard to the provided content.
getMousePos,(None),Returns current absolute mouse position in a format x,y
getClipboardContent,(None),Returns the current system clipboard content to the client.
getInputContent,(None),Returns the current text in the GUI input field to the client (the field is emptied after that)
imgToClipboard,(None),Puts a sample image into the clipboard
getDesktopSize,(None),Returns the screen dimensions of the Windows VM as "width,height"