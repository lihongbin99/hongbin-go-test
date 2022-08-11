package transfer

const (
	ErrorCode         byte = iota // 00 网络错误
	Ping                          // 01 KeepAlice
	Register                      // 02 注册到服务器, 请求NAT穿透
	RegisterSuccess               // 03 注册成功
	RegisterError                 // 04 注册失败
	Nat                           // 05 NAT请求
	NatSuccess                    // 06 NAT请求成功
	NatError                      // 07 NAT请求失败
	NewConnect                    // 08 客户端创建新连接
	NewConnectSuccess             // 09 创建新连成功
	NewConnectError               // 10 创建新连失败
	Transfer                      // 11 传输数据
	Close                         // 12 关闭连接
)
